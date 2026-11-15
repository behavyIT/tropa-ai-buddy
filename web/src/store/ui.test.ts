import { useUI } from './ui'

describe('ui store', () => {
  it('queues chat questions from other panels', () => {
    useUI.getState().ask('привет')
    expect(useUI.getState().pane).toBe('chat')
    expect(useUI.getState().takeOutbox()).toBe('привет')
    expect(useUI.getState().takeOutbox()).toBeNull()
  })

  it('shows and auto-dismisses toasts', () => {
    vi.useFakeTimers()
    useUI.getState().toast('Готово', 'success')
    expect(useUI.getState().toasts).toHaveLength(1)
    vi.advanceTimersByTime(5000)
    expect(useUI.getState().toasts).toHaveLength(0)
    vi.useRealTimers()
  })
})
