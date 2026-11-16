import { actionStatus, expandSlash, hhmm, ruNumber, setServerNow, timeAgo } from './format'
import { action } from '../test/fixtures'
import type { Action } from '../api/schemas'

const a = (extra: Record<string, unknown>) => action(extra) as unknown as Action

describe('format', () => {
  it('maps action statuses', () => {
    expect(actionStatus(a({ status: 'planned' })).label).toBe('Ждёт подтверждения')
    expect(actionStatus(a({ status: 'executing' })).tone).toBe('blue')
    expect(actionStatus(a({ status: 'failed' })).tone).toBe('red')
    expect(actionStatus(a({ status: 'cancelled' })).label).toBe('Отменено')
    const ext = ['pending_approval', 'partially_granted', 'approved', 'granted', 'rejected', 'completed', 'open', 'answered', 'other']
    const labels = ext.map((e) => actionStatus(a({ status: 'done', external_status: e })).label)
    expect(new Set(labels).size).toBe(ext.length)
  })

  it('formats relative time and clock', () => {
    const now = new Date('2026-11-09T12:00:00Z')
    expect(timeAgo('2026-11-09T11:59:50Z', now)).toBe('только что')
    expect(timeAgo('2026-11-09T11:30:00Z', now)).toBe('30 мин назад')
    expect(timeAgo('2026-11-09T09:00:00Z', now)).toBe('3 ч назад')
    expect(timeAgo('2026-11-01T09:00:00Z', now)).toMatch(/ноябр/)
    expect(timeAgo('2026-11-09T11:59:50Z')).toBeTruthy()
    expect(hhmm('2026-11-09T09:30:00+03:00')).toBe('09:30')
    setServerNow('2020-01-01T00:00:00Z')
    expect(timeAgo('2020-01-01T00:00:00Z')).toBe('только что')
    setServerNow('not a date')
    setServerNow(new Date().toISOString())
  })

  it('formats Russian numbers without Intl', () => {
    expect(ruNumber(33.333)).toBe('33,3')
    expect(ruNumber(1.25)).toBe('1,3')
    expect(ruNumber(33)).toBe('33')
    expect(ruNumber(0.04)).toBe('0')
  })

  it('expands slash commands', () => {
    expect(expandSlash('/отгул')).toBe('Как оформить отгул?')
    expect(expandSlash('/наставник где логи?')).toBe('Спроси наставника: где логи?')
    expect(expandSlash('  просто текст ')).toBe('просто текст')
    expect(expandSlash('/неизвестно')).toBe('/неизвестно')
  })
})
