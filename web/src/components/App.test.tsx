import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { App } from '../App'
import { action, buddyMessage, insights, me, route, userMessage } from '../test/fixtures'
import { mockApi, renderWithClient, resetUI } from '../test/utils'
import { useUI } from '../store/ui'

const common = {
  'GET /api/route': route,
  'GET /api/chat': { messages: [userMessage, buddyMessage()] },
  'GET /api/actions': { actions: [action({ status: 'done', external_status: 'pending_approval', external_id: 'ABS-1' })] },
  'GET /api/notifications': { notifications: [], unread: 0 },
}

beforeEach(resetUI)

describe('App', () => {
  it('[UI-01] shows login when unauthenticated and logs in with a persona', async () => {
    let authed = false
    const { calls } = mockApi({
      'GET /api/me': () => (authed ? me() : { status: 401, body: { error: { code: 'unauthorized', message: 'Нужно войти' } } }),
      'POST /api/auth/login': () => {
        authed = true
        return { user: me().user }
      },
      ...common,
    })
    renderWithClient(<App />)
    await userEvent.click(await screen.findByText('Алекс Новиков'))
    expect(calls.find((c) => c.path === '/api/auth/login')?.body).toEqual({ login: 'alex', password: 'buddy2026' })
    expect(await screen.findByText('Мой маршрут')).toBeInTheDocument()
    expect(screen.getByLabelText('Чат с Тропой')).toBeInTheDocument()
    expect(screen.getAllByText('Следующий шаг').length).toBeGreaterThan(0)
  })

  it('[UI-13] logout returns to the login screen', async () => {
    let authed = true
    mockApi({
      'GET /api/me': () => (authed ? me() : { status: 401, body: { error: { code: 'unauthorized', message: 'Нужно войти' } } }),
      'POST /api/auth/logout': () => {
        authed = false
        return {}
      },
      ...common,
    })
    renderWithClient(<App />)
    await userEvent.click(await screen.findByRole('button', { name: 'Сменить' }))
    expect(await screen.findByText('Войти как')).toBeInTheDocument()
    expect(screen.queryByText('Мой маршрут')).not.toBeInTheDocument()
  })

  it('manual login form shows server errors', async () => {
    mockApi({
      'GET /api/me': { status: 401, body: { error: { code: 'unauthorized', message: 'x' } } },
      'POST /api/auth/login': { status: 401, body: { error: { code: 'unauthorized', message: 'неверный логин или пароль' } } },
    })
    renderWithClient(<App />)
    await userEvent.type(await screen.findByLabelText('Логин'), 'alex')
    await userEvent.type(screen.getByLabelText('Пароль'), 'bad')
    await userEvent.click(screen.getByRole('button', { name: 'Войти' }))
    expect(await screen.findByText('неверный логин или пароль')).toBeInTheDocument()
  })

  it('shows a retryable error when /api/me fails', async () => {
    mockApi({ 'GET /api/me': { status: 500, body: { error: { code: 'x', message: 'Сервер упал' } } } })
    renderWithClient(<App />)
    expect(await screen.findByText('Сервер упал')).toBeInTheDocument()
  })

  it('intern mobile tabs switch panes', async () => {
    mockApi({ 'GET /api/me': me(), ...common })
    renderWithClient(<App />)
    await userEvent.click(await screen.findByRole('button', { name: 'Маршрут' }))
    expect(useUI.getState().pane).toBe('route')
  })

  it('[UI-02] lead sees staff tabs and navigates between them', async () => {
    mockApi({
      'GET /api/me': me('lead'),
      'GET /api/notifications': { notifications: [], unread: 0 },
      'GET /api/approvals': { approvals: [] },
      'GET /api/team': { interns: [] },
      'GET /api/mentor/questions': { questions: [] },
      'GET /api/insights': insights,
      'GET /api/flags': { flags: me().flags, team: 'ads-backend' },
      'GET /api/chat': { messages: [] },
      'GET /api/actions': { actions: [] },
    })
    renderWithClient(<App />)
    expect(await screen.findByText('Нет заявок на согласование')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('tab', { name: 'Команда' }))
    expect(await screen.findByText('Нет стажёров')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('tab', { name: 'Вопросы' }))
    expect(await screen.findByText(/Новых вопросов нет/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('tab', { name: 'Тропинки' }))
    expect(await screen.findByText(/Тропинки · /)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('tab', { name: 'Флаги' }))
    expect(await screen.findByText('Сценарии и фича-флаги')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('tab', { name: 'Чат' }))
    expect(await screen.findByText(/Я Тропа — твой AI-бадди/)).toBeInTheDocument()
  })

  it('mentor and hrbp get their default tabs', async () => {
    mockApi({ 'GET /api/me': me('mentor'), 'GET /api/notifications': { notifications: [], unread: 0 }, 'GET /api/mentor/questions': { questions: [] } })
    const { unmount } = renderWithClient(<App />)
    expect(await screen.findByText('Сводка вопросов')).toBeInTheDocument()
    unmount()
    resetUI()
    mockApi({ 'GET /api/me': me('hrbp'), 'GET /api/notifications': { notifications: [], unread: 0 }, 'GET /api/insights': insights })
    renderWithClient(<App />)
    expect(await screen.findByText(/Тропинки · /)).toBeInTheDocument()
    await waitFor(() => expect(useUI.getState().tab).toBe('insights'))
  })
})
