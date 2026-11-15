import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Approvals } from './Approvals'
import { FlagsPanel } from './FlagsPanel'
import { Insights } from './Insights'
import { MentorInbox } from './MentorInbox'
import { NowPanel } from './NowPanel'
import { RoutePanel } from './RoutePanel'
import { TeamPanel } from './TeamPanel'
import { TopBar } from './TopBar'
import { action, demo, insights, me, route } from '../test/fixtures'
import { mockApi, renderWithClient, resetUI } from '../test/utils'
import { useUI } from '../store/ui'
import type { Me } from '../api/schemas'
import { MeSchema } from '../api/schemas'

beforeEach(resetUI)

describe('RoutePanel', () => {
  it('[UI-12] renders stages, statuses and task actions', async () => {
    const { calls } = mockApi({ 'GET /api/route': route, 'POST /api/route/tasks/meet-mentor/start': {}, 'POST /api/route/tasks/meet-mentor/complete': {}, 'POST /api/route/tasks/add-to-chats/nudge': {} })
    renderWithClient(<RoutePanel />)
    expect(await screen.findByText('Мой маршрут')).toBeInTheDocument()
    expect(screen.getByText('Просрочено пунктов: 1')).toBeInTheDocument()
    expect(screen.getByText('отвечает: Артём Ким')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Начать' }))
    await userEvent.click(screen.getByRole('button', { name: 'Готово' }))
    await userEvent.click(screen.getByRole('button', { name: 'Напомнить' }))
    await waitFor(() => expect(calls.filter((c) => c.method === 'POST')).toHaveLength(3))
    await userEvent.click(screen.getByRole('button', { name: 'Сдать в чате' }))
    expect(useUI.getState().outbox).toBe('Хочу рассказать про Sentry')
    await userEvent.click(screen.getByRole('button', { name: 'Запросить' }))
    expect(useUI.getState().outbox).toBe('Запроси доступ: gitlab, jira')
    await userEvent.click(screen.getByRole('button', { name: 'Материал' }))
    expect(useUI.getState().outbox).toMatch(/Как и кого спрашивать/)
    await userEvent.click(screen.getByRole('button', { name: /Неделя 1/ }))
    expect(screen.getByText('Ждёт: Артём Ким')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /Первые дни/ }))
  })

  it('shows errors from task actions and route loading', async () => {
    mockApi({ 'GET /api/route': route, 'POST /api/route/tasks/add-to-chats/nudge': { status: 429, body: { error: { code: 'rate_limited', message: 'Уже напомнили' } } } })
    renderWithClient(<RoutePanel />)
    await userEvent.click(await screen.findByRole('button', { name: 'Напомнить' }))
    await waitFor(() => expect(useUI.getState().toasts.some((t) => t.text === 'Уже напомнили')).toBe(true))
    mockApi({ 'GET /api/route?user=x': { status: 404, body: { error: { code: 'not_found', message: 'стажёр не найден' } } } })
    renderWithClient(<RoutePanel user="x" />)
    expect(await screen.findByText('стажёр не найден')).toBeInTheDocument()
  })

  it('read-only mode hides actions', async () => {
    mockApi({ 'GET /api/route': route })
    renderWithClient(<RoutePanel readOnly />)
    await screen.findByText('Мой маршрут')
    expect(screen.queryByRole('button', { name: 'Начать' })).not.toBeInTheDocument()
  })
})

describe('NowPanel', () => {
  it('shows next step, waiting list and receipts', async () => {
    mockApi({ 'GET /api/route': route, 'GET /api/actions': { actions: [action({ status: 'done', external_status: 'granted', external_id: 'ACC-1' }), action({ id: 'p', status: 'planned' })] } })
    renderWithClient(<NowPanel />)
    expect(await screen.findByText('Познакомься с наставником')).toBeInTheDocument()
    expect(screen.getByText('Ждём')).toBeInTheDocument()
    expect(screen.getByText('Пока ждёшь — можно сделать')).toBeInTheDocument()
    expect(await screen.findByText('ACC-1')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Помоги с этим' }))
    expect(useUI.getState().outbox).toMatch(/Познакомься/)
  })

  it('celebrates when nothing is left', async () => {
    mockApi({ 'GET /api/route': { ...route, next: [], waiting: [], while_waiting: [] }, 'GET /api/actions': { actions: [] } })
    renderWithClient(<NowPanel />)
    expect(await screen.findByText(/Все доступные шаги закрыты/)).toBeInTheDocument()
    expect(screen.getByText(/Здесь появятся заявки/)).toBeInTheDocument()
  })
})

describe('TopBar', () => {
  const meData = MeSchema.parse(me()) as Me
  it('[UI-11] runs the time machine, demo menu and notifications', async () => {
    const { calls } = mockApi({
      'GET /api/notifications': { notifications: [{ id: 'n1', kind: 'brief', title: 'День 2: бриф', body: 'Главное', created_at: '2026-11-10T09:30:00+03:00' }], unread: 1 },
      'POST /api/notifications/read': {},
      'POST /api/demo/advance': demo,
      'POST /api/demo/reset': demo,
      'POST /api/demo/corp-fault': demo,
      'POST /api/auth/logout': {},
    })
    renderWithClient(<TopBar me={meData} />)
    expect(screen.getByText(/День 1 · пн, 9 ноября/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: /\+1 день/ }))
    await waitFor(() => expect(useUI.getState().toasts.some((t) => /Машина времени/.test(t.text))).toBe(true))
    for (const item of ['+7 дней', 'Имитировать сбой сервисов', 'Сбросить демо']) {
      await userEvent.click(screen.getByRole('button', { name: 'Демо-меню' }))
      await userEvent.click(screen.getByRole('menuitem', { name: item }))
    }
    await waitFor(() => expect(calls.filter((c) => c.path.startsWith('/api/demo')).length).toBe(4))
    await userEvent.click(await screen.findByRole('button', { name: /Уведомления: 1/ }))
    expect(screen.getByText('Главное')).toBeInTheDocument()
    await waitFor(() => expect(calls.some((c) => c.path === '/api/notifications/read')).toBe(true))
    await userEvent.click(screen.getByRole('button', { name: 'Сменить' }))
    await waitFor(() => expect(calls.some((c) => c.path === '/api/auth/logout')).toBe(true))
  })

  it('shows outage badge, LLM name and empty notifications', async () => {
    mockApi({ 'GET /api/notifications': { notifications: [], unread: 0 }, 'POST /api/demo/corp-fault': demo })
    const m = { ...meData, llm: 'anthropic:claude-opus-5-5', demo: { ...demo, corp_fault: true } }
    renderWithClient(<TopBar me={m} />)
    expect(screen.getByText('Сбой сервисов')).toBeInTheDocument()
    expect(screen.getByText('anthropic:claude-opus-5-5')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Демо-меню' }))
    await userEvent.click(screen.getByRole('menuitem', { name: 'Выключить сбой сервисов' }))
    await userEvent.click(screen.getByRole('button', { name: /Уведомления: 0/ }))
    expect(screen.getByText('Пока тихо')).toBeInTheDocument()
  })
})

describe('MentorInbox', () => {
  it('answers questions and publishes to the knowledge base', async () => {
    const { calls } = mockApi({
      'GET /api/mentor/questions': {
        questions: [
          { id: 'q1', asker_id: 'a', text: 'Где логи?', context: 'День 3', urgent: true, status: 'open', created_at: '2026-11-09T10:00:00+03:00', asker_names: ['Алекс', 'Дана'], ask_count: 2 },
          { id: 'q2', asker_id: 'a', text: 'Старый', context: '', urgent: false, status: 'answered', answer: 'Так', card_id: 'c', created_at: '2026-11-09T10:00:00+03:00', asker_names: ['Алекс'], ask_count: 1 },
        ],
      },
      'POST /api/mentor/questions/q1/answer': {},
    })
    renderWithClient(<MentorInbox />)
    expect(await screen.findByText('Где логи?')).toBeInTheDocument()
    expect(screen.getByText('спросили 2')).toBeInTheDocument()
    expect(screen.getByText('в базе знаний')).toBeInTheDocument()
    await userEvent.type(screen.getByLabelText('Ответ'), 'В Grafana')
    await userEvent.click(screen.getByLabelText(/Сохранить ответ/))
    await userEvent.click(screen.getByRole('button', { name: 'Ответить' }))
    await waitFor(() => expect(calls.find((c) => c.method === 'POST')?.body).toEqual({ answer: 'В Grafana', publish: false }))
  })

  it('handles errors', async () => {
    mockApi({ 'GET /api/mentor/questions': { status: 403, body: { error: { code: 'forbidden', message: 'Только наставникам' } } } })
    renderWithClient(<MentorInbox />)
    expect(await screen.findByText('Только наставникам')).toBeInTheDocument()
  })
})

describe('TeamPanel', () => {
  it('lists interns and opens a route', async () => {
    mockApi({ 'GET /api/team': { interns: insights.interns }, 'GET /api/route?user=u1': route })
    renderWithClient(<TeamPanel />)
    await userEvent.click(await screen.findByRole('button', { name: /Алекс/ }))
    expect(await screen.findByText('Алекс Новиков')).toBeInTheDocument()
    expect(screen.getByText(/просрочено 1/)).toBeInTheDocument()
  })
  it('shows load errors', async () => {
    mockApi({ 'GET /api/team': { status: 500, body: { error: { code: 'x', message: 'Ошибка команды' } } } })
    renderWithClient(<TeamPanel />)
    expect(await screen.findByText('Ошибка команды')).toBeInTheDocument()
  })
})

describe('Approvals', () => {
  it('approves and rejects', async () => {
    const appr = { action: action({ status: 'done', external_status: 'pending_approval' }), requester: 'Алекс Новиков', kind: 'Отгул', details: 'пт, 13 ноября' }
    const { calls } = mockApi({ 'GET /api/approvals': { approvals: [appr] }, 'POST /api/approvals/a1/decide': action({ status: 'done' }) })
    renderWithClient(<Approvals />)
    await userEvent.type(await screen.findByLabelText('Комментарий'), 'ок')
    await userEvent.click(screen.getByRole('button', { name: 'Согласовать' }))
    await userEvent.click(screen.getByRole('button', { name: 'Отклонить' }))
    await waitFor(() => expect(calls.filter((c) => c.method === 'POST').map((c) => (c.body as { approve: boolean }).approve)).toEqual([true, false]))
  })
  it('shows decision errors and load errors', async () => {
    const appr = { action: action({ status: 'done', external_status: 'pending_approval', view: { ...action().view, warnings: [] } }), requester: 'А', kind: 'Доступ', details: 'k8s' }
    mockApi({ 'GET /api/approvals': { approvals: [appr] }, 'POST /api/approvals/a1/decide': { status: 409, body: { error: { code: 'conflict', message: 'Уже рассмотрена' } } } })
    renderWithClient(<Approvals />)
    await userEvent.click(await screen.findByRole('button', { name: 'Согласовать' }))
    await waitFor(() => expect(useUI.getState().toasts.some((t) => t.text === 'Уже рассмотрена')).toBe(true))
    mockApi({ 'GET /api/approvals': { status: 403, body: { error: { code: 'forbidden', message: 'Только руководителю' } } } })
    renderWithClient(<Approvals />)
    expect(await screen.findByText('Только руководителю')).toBeInTheDocument()
  })
})

describe('Insights', () => {
  it('renders tiles, hotspots, gaps and method', async () => {
    mockApi({ 'GET /api/insights': insights })
    renderWithClient(<Insights />)
    expect(await screen.findByText('70%')).toBeInTheDocument()
    expect(screen.getByText('Доступ к k8s')).toBeInTheDocument()
    expect(screen.getByText('staging токен')).toBeInTheDocument()
    expect(screen.getByText(/Как проверяем эффект/)).toBeInTheDocument()
    expect(screen.getByText('33,3 ч')).toBeInTheDocument()
  })
  it('handles empty metrics and errors', async () => {
    mockApi({ 'GET /api/insights': { ...insights, metrics: { ...insights.metrics, avg_access_wait_hours: 0, actions_failed: 0 } } })
    renderWithClient(<Insights />)
    expect(await screen.findByText('без сбоев')).toBeInTheDocument()
    mockApi({ 'GET /api/insights': { status: 403, body: { error: { code: 'forbidden', message: 'Нет доступа' } } } })
    renderWithClient(<Insights />)
    expect(await screen.findByText('Нет доступа')).toBeInTheDocument()
  })
})

describe('FlagsPanel', () => {
  it('toggles a team flag', async () => {
    const meLead = MeSchema.parse(me('lead')) as Me
    const { calls } = mockApi({ 'GET /api/flags': { flags: me().flags, team: 'ads-backend' }, 'PUT /api/flags/teachback': { flags: me().flags, team: 'ads-backend' } })
    renderWithClient(<FlagsPanel me={meLead} />)
    await userEvent.click(await screen.findByLabelText('Режим «Расскажи мне»'))
    await waitFor(() => expect(calls.find((c) => c.method === 'PUT')?.body).toEqual({ enabled: true, scope: 'team:ads-backend' }))
  })
  it('uses global scope for HR BP and reports errors', async () => {
    const hr = MeSchema.parse(me('hrbp')) as Me
    mockApi({ 'GET /api/flags': { flags: me().flags, team: '' }, 'PUT /api/flags/actions.absence': { status: 403, body: { error: { code: 'forbidden', message: 'Нет прав' } } } })
    renderWithClient(<FlagsPanel me={hr} />)
    expect(await screen.findByText(/Глобальные значения/)).toBeInTheDocument()
    await userEvent.click(screen.getByLabelText('Оформление отгула'))
    await waitFor(() => expect(useUI.getState().toasts.some((t) => t.text === 'Нет прав')).toBe(true))
    mockApi({ 'GET /api/flags': { status: 500, body: { error: { code: 'x', message: 'Флаги недоступны' } } } })
    renderWithClient(<FlagsPanel me={hr} />)
    expect(await screen.findByText('Флаги недоступны')).toBeInTheDocument()
  })
})
