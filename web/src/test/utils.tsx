import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { render } from '@testing-library/react'
import type { ReactElement } from 'react'
import { vi } from 'vitest'
import { useUI } from '../store/ui'

export type Handler = (body: unknown, url: URL) => { status?: number; body: unknown } | unknown

export interface Call {
  method: string
  path: string
  body: unknown
  headers: Record<string, string>
}

/** Installs a fetch mock routing "METHOD /path" (query ignored unless present in the key). */
export function mockApi(routes: Record<string, Handler | object>) {
  const calls: Call[] = []
  const fn = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), 'http://localhost')
    const method = (init?.method ?? 'GET').toUpperCase()
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    calls.push({ method, path: url.pathname + url.search, body, headers: (init?.headers ?? {}) as Record<string, string> })
    const key = [`${method} ${url.pathname}${url.search}`, `${method} ${url.pathname}`].find((k) => k in routes)
    if (!key) return new Response(JSON.stringify({ error: { code: 'not_found', message: 'нет мока ' + method + ' ' + url.pathname } }), { status: 404 })
    const h = routes[key]
    const out = typeof h === 'function' ? (h as Handler)(body, url) : h
    const res = out && typeof out === 'object' && 'body' in (out as object) ? (out as { status?: number; body: unknown }) : { status: 200, body: out }
    return new Response(JSON.stringify(res.body), { status: res.status ?? 200, headers: { 'Content-Type': 'application/json' } })
  })
  vi.stubGlobal('fetch', fn)
  return { calls, fn }
}

export function renderWithClient(ui: ReactElement) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, refetchInterval: false, staleTime: 0 }, mutations: { retry: false } } })
  return { qc, ...render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>) }
}

export function resetUI() {
  useUI.setState({ tab: '', pane: 'chat', viewIntern: null, draft: '', outbox: null, toasts: [] })
}
