import { act, screen, waitFor } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import { Chat, renderText } from './Chat'
import { action, buddyMessage, userMessage } from '../test/fixtures'
import { mockApi, renderWithClient, resetUI } from '../test/utils'
import { useUI } from '../store/ui'
import { render } from '@testing-library/react'

beforeEach(resetUI)

const reply = (text: string) => ({
  user: { ...userMessage, id: 'u9', text },
  buddy: buddyMessage({ id: 'b9', text: 'Ответ на ' + text, payload: { ...buddyMessage().payload, cards: [], citations: [], suggestions: ['Дальше'] } }),
  actions: [action({ id: 'a9' })],
})

describe('Chat', () => {
  it('shows welcome and starter chips for an empty history', async () => {
    const { calls } = mockApi({ 'GET /api/chat': { messages: [] }, 'POST /api/chat': (b) => reply((b as { text: string }).text), 'GET /api/actions': { actions: [] } })
    renderWithClient(<Chat firstName="Алекс" />)
    expect(await screen.findByText(/Привет, Алекс!/)).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Как оформить отгул?' }))
    expect(await screen.findByText('Ответ на Как оформить отгул?')).toBeInTheDocument()
    expect(calls.some((c) => c.path === '/api/chat' && c.method === 'POST')).toBe(true)
  })

  it('[UI-05] renders citations, all card types and feedback', async () => {
    const { calls } = mockApi({
      'GET /api/chat': { messages: [userMessage, buddyMessage()] },
      'GET /api/actions': { actions: [action()] },
      'POST /api/chat/messages/m2/feedback': buddyMessage({ feedback: 1 }),
    })
    renderWithClient(<Chat firstName="Алекс" />)
    expect(await screen.findByText('Документ')).toBeInTheDocument()
    expect(screen.getByText('Тестовая инструкция')).toBeInTheDocument()
    expect(screen.getByText(/Ответ наставника · Артём Ким/)).toBeInTheDocument()
    expect(screen.getAllByText('Артём Ким').length).toBeGreaterThan(0)
    expect(screen.getByText('«Расскажи о Sentry»')).toBeInTheDocument()
    expect(screen.getByText('Подсказка')).toBeInTheDocument()
    expect(screen.getByText('Мои заявки')).toBeInTheDocument()
    expect(screen.getByText('LLM')).toBeInTheDocument()
    await userEvent.click(screen.getByLabelText('Полезно'))
    await waitFor(() => expect(calls.some((c) => c.path.endsWith('/feedback'))).toBe(true))
    await userEvent.click(screen.getByRole('button', { name: 'Шаг один' }))
    await waitFor(() => expect(useUI.getState().outbox).toBeNull())
  })

  it('sends with Enter, expands slash commands and shows suggestions', async () => {
    const { calls } = mockApi({ 'GET /api/chat': { messages: [] }, 'POST /api/chat': (b) => reply((b as { text: string }).text), 'GET /api/actions': { actions: [] } })
    renderWithClient(<Chat firstName="Алекс" />)
    const box = await screen.findByLabelText('Сообщение')
    await userEvent.type(box, '/от')
    expect(screen.getByRole('listbox', { name: 'Команды' })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('option', { name: /\/отгул/ }))
    await userEvent.type(box, '{Enter}')
    expect(await screen.findByText('Ответ на Как оформить отгул?')).toBeInTheDocument()
    await userEvent.type(box, 'строка{Shift>}{Enter}{/Shift}вторая')
    expect((box as HTMLTextAreaElement).value).toBe('строка\nвторая')
    await userEvent.click(screen.getByRole('button', { name: 'Отправить' }))
    await userEvent.click(await screen.findByRole('button', { name: 'Дальше' }))
    await waitFor(() => expect(calls.filter((c) => c.method === 'POST').length).toBe(3))
  })

  it('[UI-03] shows pending state and retries after errors', async () => {
    let fail = true
    mockApi({
      'GET /api/chat': { messages: [] },
      'GET /api/actions': { actions: [] },
      'POST /api/chat': (b) => (fail ? { status: 503, body: { error: { code: 'unavailable', message: 'Сервис недоступен' } } } : reply((b as { text: string }).text)),
    })
    renderWithClient(<Chat firstName="Алекс" />)
    await userEvent.type(await screen.findByLabelText('Сообщение'), 'вопрос{Enter}')
    expect(await screen.findByText('Сервис недоступен')).toBeInTheDocument()
    fail = false
    await userEvent.click(screen.getByRole('button', { name: 'Повторить' }))
    expect(await screen.findByText('Ответ на вопрос')).toBeInTheDocument()
  })

  it('[UI-04] cancel aborts an in-flight message', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn((url: string, init: RequestInit) => {
        if (String(url).includes('/api/chat') && init?.method === 'POST') {
          return new Promise((_, rej) => init.signal?.addEventListener('abort', () => rej(new DOMException('a', 'AbortError'))))
        }
        return Promise.resolve(new Response(JSON.stringify(String(url).includes('actions') ? { actions: [] } : { messages: [] }), { status: 200 }))
      }),
    )
    renderWithClient(<Chat firstName="Алекс" />)
    await userEvent.type(await screen.findByLabelText('Сообщение'), 'долгий{Enter}')
    expect(await screen.findByText('Тропа думает…')).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Отменить' }))
    await waitFor(() => expect(screen.queryByText('Тропа думает…')).not.toBeInTheDocument())
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('sends questions queued from other panels', async () => {
    mockApi({ 'GET /api/chat': { messages: [] }, 'POST /api/chat': (b) => reply((b as { text: string }).text), 'GET /api/actions': { actions: [] } })
    renderWithClient(<Chat firstName="Алекс" />)
    await screen.findByLabelText('Сообщение')
    act(() => useUI.getState().ask('Из маршрута'))
    expect(await screen.findByText('Ответ на Из маршрута')).toBeInTheDocument()
  })

  it('shows an error box when history fails', async () => {
    mockApi({ 'GET /api/chat': { status: 500, body: { error: { code: 'x', message: 'История недоступна' } } } })
    renderWithClient(<Chat firstName="Алекс" />)
    expect(await screen.findByText('История недоступна')).toBeInTheDocument()
  })

  it('[UI-06] renders citation markers safely', () => {
    render(<div>{renderText('a [1] <b>x</b>\n\nb')}</div>)
    expect(screen.getByText('1').tagName).toBe('SUP')
    expect(screen.getByText(/<b>x<\/b>/)).toBeInTheDocument()
  })
})
