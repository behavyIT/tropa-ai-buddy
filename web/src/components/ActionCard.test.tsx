import { screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { ActionCard, parseDates } from './ActionCard'
import { action } from '../test/fixtures'
import { mockApi, renderWithClient, resetUI } from '../test/utils'
import { useUI } from '../store/ui'
import type { Action } from '../api/schemas'

beforeEach(resetUI)

describe('ActionCard', () => {
  it('[UI-07] confirms a planned action and shows the receipt', async () => {
    let state = action()
    mockApi({
      'GET /api/actions': () => ({ actions: [state] }),
      'POST /api/actions/a1/confirm': () => {
        state = action({ status: 'done', external_status: 'pending_approval', external_id: 'ABS-00001' })
        return state
      },
    })
    renderWithClient(<ActionCard id="a1" />)
    expect(await screen.findByText('Демо спринта в 16:00', { exact: false })).toBeInTheDocument()
    expect(screen.getByText(/Только за себя/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Отправить заявку' }))
    expect(await screen.findByText('ABS-00001')).toBeInTheDocument()
    expect(screen.getByText('На согласовании')).toBeInTheDocument()
    expect(useUI.getState().toasts[0].text).toMatch(/Готово/)
  })

  it('shows failures with retry and supports cancel', async () => {
    let state = action({ status: 'failed', error: 'Сервис недоступен' })
    mockApi({
      'GET /api/actions': () => ({ actions: [state] }),
      'POST /api/actions/a1/confirm': () => action({ status: 'failed', error: 'Опять недоступен' }),
      'POST /api/actions/a1/cancel': () => {
        state = action({ status: 'cancelled' })
        return state
      },
    })
    renderWithClient(<ActionCard id="a1" />)
    expect(await screen.findByText('Сервис недоступен')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Повторить' }))
    await waitFor(() => expect(useUI.getState().toasts.some((t) => t.text === 'Опять недоступен')).toBe(true))
    await userEvent.click(screen.getByRole('button', { name: 'Отменить' }))
    expect(await screen.findByText('Отменено')).toBeInTheDocument()
  })

  it('[UI-08] edits absence dates before confirming', async () => {
    const { calls } = mockApi({
      'GET /api/actions': { actions: [action()] },
      'PATCH /api/actions/a1': (b) => action({ title: 'Отгул: изменён', view: { ...action().view, fields: [{ label: 'Даты', value: 'ср, 18 ноября' }] }, ...(b as object) }),
    })
    renderWithClient(<ActionCard id="a1" />)
    await userEvent.click(await screen.findByRole('button', { name: 'Изменить' }))
    const from = screen.getByLabelText('С') as HTMLInputElement
    expect(from.value).toBe('2026-11-13')
    await userEvent.clear(from)
    await userEvent.type(from, '2026-11-18')
    await userEvent.clear(screen.getByLabelText('Комментарий'))
    await userEvent.type(screen.getByLabelText('Комментарий'), 'Семья')
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }))
    await waitFor(() => expect(calls.find((c) => c.method === 'PATCH')?.body).toMatchObject({ date_from: '2026-11-18', comment: 'Семья' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Изменить' }))
    await userEvent.click(screen.getByRole('button', { name: 'Назад' }))
    expect(screen.queryByLabelText('С')).not.toBeInTheDocument()
  })

  it('reports edit errors and missing actions', async () => {
    mockApi({
      'GET /api/actions': { actions: [action()] },
      'PATCH /api/actions/a1': { status: 400, body: { error: { code: 'bad', message: 'Эта дата уже прошла' } } },
      'POST /api/actions/a1/confirm': { status: 409, body: { error: { code: 'conflict', message: 'Уже отменено' } } },
    })
    renderWithClient(<ActionCard id="a1" />)
    await userEvent.click(await screen.findByRole('button', { name: 'Отправить заявку' }))
    await waitFor(() => expect(useUI.getState().toasts.some((t) => t.text === 'Уже отменено')).toBe(true))
    await userEvent.click(screen.getByRole('button', { name: 'Изменить' }))
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }))
    await waitFor(() => expect(useUI.getState().toasts.some((t) => t.text === 'Эта дата уже прошла')).toBe(true))
    const r = renderWithClient(<ActionCard id="missing" />)
    expect(await r.findByText('Действие недоступно или отменено')).toBeInTheDocument()
  })

  it('parses date ranges back from the plan', () => {
    const a = action({ view: { ...action().view, fields: [{ label: 'Даты', value: 'ср, 18 ноября — чт, 19 ноября' }] } }) as unknown as Action
    expect(parseDates(a)).toEqual({ date_from: '2026-11-18', date_to: '2026-11-19', comment: '' })
  })
})
