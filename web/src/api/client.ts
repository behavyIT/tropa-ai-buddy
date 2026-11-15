import type { z } from 'zod'

/** Error returned by the API (or synthesised for network failures). */
export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string,
    message: string,
  ) {
    super(message)
    this.name = 'ApiError'
  }
}

export interface RequestOptions {
  signal?: AbortSignal
  timeoutMs?: number
}

/** Base URL of the API. Same-origin by default; set VITE_API_URL when the UI is hosted separately. */
export const API_BASE: string = (import.meta.env.VITE_API_URL as string | undefined) ?? ''

/**
 * Explicit API client: JSON in/out, CSRF header on mutations, timeout,
 * cancellation and runtime validation of every response with Zod.
 */
export async function request<T>(
  method: 'GET' | 'POST' | 'PUT' | 'PATCH',
  path: string,
  schema: z.ZodType<T, z.ZodTypeDef, unknown>,
  body?: unknown,
  opts: RequestOptions = {},
): Promise<T> {
  const ctrl = new AbortController()
  const onAbort = () => ctrl.abort(opts.signal?.reason)
  opts.signal?.addEventListener('abort', onAbort)
  const timer = setTimeout(() => ctrl.abort(new DOMException('timeout', 'TimeoutError')), opts.timeoutMs ?? 15000)
  try {
    const headers: Record<string, string> = { Accept: 'application/json' }
    if (method !== 'GET') {
      headers['Content-Type'] = 'application/json'
      headers['X-Requested-With'] = 'tropa'
    }
    let res: Response
    try {
      res = await fetch(API_BASE + path, {
        method,
        headers,
        credentials: 'include',
        body: method === 'GET' ? undefined : JSON.stringify(body ?? {}),
        signal: ctrl.signal,
      })
    } catch (e) {
      if (opts.signal?.aborted) throw new ApiError(0, 'aborted', 'Запрос отменён')
      if (ctrl.signal.aborted) throw new ApiError(0, 'timeout', 'Сервер долго не отвечает. Попробуйте ещё раз.')
      throw new ApiError(0, 'network', 'Нет связи с сервером. Проверьте подключение.')
    }
    const data: unknown = await res.json().catch(() => ({}))
    if (!res.ok) {
      const err = (data as { error?: { code?: string; message?: string } }).error
      throw new ApiError(res.status, err?.code ?? 'error', err?.message ?? `Ошибка ${res.status}`)
    }
    const parsed = schema.safeParse(data)
    if (!parsed.success) {
      throw new ApiError(res.status, 'invalid_response', 'Сервер вернул неожиданный ответ')
    }
    return parsed.data
  } finally {
    clearTimeout(timer)
    opts.signal?.removeEventListener('abort', onAbort)
  }
}
