import { ApiError, toApiError } from './errors.ts'

export const apiBase = '/api/v1'

type Method = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE'

export type Query = Record<string, string | number | boolean | null | undefined>

export interface RequestOptions {
  query?: Query
  signal?: AbortSignal
  // false untuk request yang 401-nya bagian dari alur normal (cek sesi, login).
  handleUnauthenticated?: boolean
}

export interface ClientHooks {
  csrfToken: string | null
  onUnauthenticated?: () => void
  // Mengambil token CSRF terbaru (misalnya setelah sesi dirotasi); null jika sesi sudah tidak ada.
  refreshCsrf?: () => Promise<string | null>
}

const mutating = new Set<Method>(['POST', 'PUT', 'PATCH', 'DELETE'])

// Setiap segmen path di-encode supaya nama schema/tabel dengan karakter khusus tetap aman.
export function path(strings: TemplateStringsArray, ...values: (string | number)[]): string {
  return strings.reduce((out, part, i) => out + part + (i < values.length ? encodeURIComponent(String(values[i])) : ''), '')
}

export function buildUrl(target: string, query?: Query): string {
  if (!target.startsWith('/')) throw new Error(`path API harus diawali "/": ${target}`)
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(query ?? {})) {
    if (value !== undefined && value !== null && value !== '') params.append(key, String(value))
  }
  const search = params.toString()
  return apiBase + target + (search ? `?${search}` : '')
}

function isAbort(err: unknown, signal?: AbortSignal): boolean {
  return signal?.aborted === true || (err instanceof DOMException && err.name === 'AbortError')
}

async function parseBody<T>(res: Response): Promise<T> {
  if (res.status === 204 || res.headers.get('Content-Length') === '0') return undefined as T
  const type = res.headers.get('Content-Type') ?? ''
  if (!type.includes('application/json')) return undefined as T
  return (await res.json()) as T
}

// Klien JSON untuk /api/v1: cookie sesi same-origin, header X-CSRF-Token untuk mutasi, dan error dalam bentuk ApiError.
export class ApiClient {
  private hooks: ClientHooks = { csrfToken: null }
  private readonly fetcher: typeof fetch

  constructor(fetcher?: typeof fetch) {
    this.fetcher = fetcher ?? ((input, init) => fetch(input, init))
  }

  configure(hooks: Partial<ClientHooks>) {
    this.hooks = { ...this.hooks, ...hooks }
  }

  get<T>(target: string, options?: RequestOptions) {
    return this.request<T>('GET', target, undefined, options)
  }

  post<T>(target: string, body?: unknown, options?: RequestOptions) {
    return this.request<T>('POST', target, body, options)
  }

  put<T>(target: string, body?: unknown, options?: RequestOptions) {
    return this.request<T>('PUT', target, body, options)
  }

  patch<T>(target: string, body?: unknown, options?: RequestOptions) {
    return this.request<T>('PATCH', target, body, options)
  }

  delete<T>(target: string, options?: RequestOptions) {
    return this.request<T>('DELETE', target, undefined, options)
  }

  async request<T>(method: Method, target: string, body?: unknown, options: RequestOptions = {}): Promise<T> {
    let res = await this.send(method, target, body, options)
    if (!res.ok && res.status === 403 && mutating.has(method) && this.hooks.refreshCsrf) {
      const err = await toApiError(res.clone())
      // Token CSRF bisa basi setelah rotasi sesi; ambil yang baru lalu ulangi sekali saja.
      if (err.code === 'csrf_failed') {
        const token = await this.hooks.refreshCsrf()
        if (!token) {
          if (options.handleUnauthenticated !== false) this.hooks.onUnauthenticated?.()
          throw err
        }
        this.hooks.csrfToken = token
        res = await this.send(method, target, body, options)
      }
    }
    if (res.ok) return parseBody<T>(res)
    const err = await toApiError(res)
    if (err.status === 401 && options.handleUnauthenticated !== false) this.hooks.onUnauthenticated?.()
    throw err
  }

  private async send(method: Method, target: string, body: unknown, options: RequestOptions): Promise<Response> {
    const headers: Record<string, string> = { Accept: 'application/json' }
    if (body !== undefined) headers['Content-Type'] = 'application/json'
    if (mutating.has(method) && this.hooks.csrfToken) headers['X-CSRF-Token'] = this.hooks.csrfToken
    try {
      return await this.fetcher(buildUrl(target, options.query), {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body),
        credentials: 'same-origin',
        signal: options.signal,
      })
    } catch (err) {
      // Pembatalan dilempar ulang apa adanya supaya TanStack Query mengenalinya.
      if (isAbort(err, options.signal)) throw err
      throw new ApiError({ status: 0, code: 'network', message: '' })
    }
  }
}

export const api = new ApiClient()
