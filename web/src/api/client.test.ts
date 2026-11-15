import { z } from 'zod'
import { ApiError, request } from './client'
import { mockApi } from '../test/utils'

const S = z.object({ ok: z.boolean() })

describe('API client', () => {
  it('GET returns parsed data without CSRF header', async () => {
    const { calls } = mockApi({ 'GET /x': { ok: true } })
    await expect(request('GET', '/x', S)).resolves.toEqual({ ok: true })
    expect(calls[0].headers['X-Requested-With']).toBeUndefined()
  })

  it('[UI-10] POST sends JSON body and CSRF header', async () => {
    const { calls } = mockApi({ 'POST /x': { ok: true } })
    await request('POST', '/x', S, { a: 1 })
    expect(calls[0].body).toEqual({ a: 1 })
    expect(calls[0].headers['X-Requested-With']).toBe('tropa')
  })

  it('maps API errors', async () => {
    mockApi({ 'GET /x': { status: 403, body: { error: { code: 'forbidden', message: 'Нельзя' } } } })
    await expect(request('GET', '/x', S)).rejects.toMatchObject({ status: 403, code: 'forbidden', message: 'Нельзя' })
  })

  it('handles non-JSON errors and invalid responses', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => new Response('oops', { status: 500 })))
    await expect(request('GET', '/x', S)).rejects.toMatchObject({ status: 500, message: 'Ошибка 500' })
    mockApi({ 'GET /x': { nope: 1 } })
    await expect(request('GET', '/x', S)).rejects.toMatchObject({ code: 'invalid_response' })
  })

  it('[UI-09] distinguishes network errors, timeouts and cancellation', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('fail') }))
    await expect(request('GET', '/x', S)).rejects.toMatchObject({ code: 'network' })
    vi.stubGlobal(
      'fetch',
      vi.fn((_u: unknown, init: RequestInit) => new Promise((_, rej) => init.signal?.addEventListener('abort', () => rej(new DOMException('a', 'AbortError'))))),
    )
    await expect(request('GET', '/x', S, undefined, { timeoutMs: 5 })).rejects.toMatchObject({ code: 'timeout' })
    const c = new AbortController()
    const p = request('GET', '/x', S, undefined, { signal: c.signal })
    c.abort()
    await expect(p).rejects.toBeInstanceOf(ApiError)
    await expect(p).rejects.toMatchObject({ code: 'aborted' })
  })
})
