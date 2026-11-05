import { describe, expect, it, vi } from 'vitest'
import { ApiClient, buildUrl, path } from './client.ts'
import { ApiError } from './errors.ts'

function json(status: number, body: unknown, headers: Record<string, string> = {}) {
  return new Response(JSON.stringify(body), { status, headers: { 'Content-Type': 'application/json', ...headers } })
}

function errorBody(code: string, message = '', extra: Record<string, unknown> = {}) {
  return { error: { code, message, request_id: 'req-1', ...extra } }
}

function setup(...responses: (Response | Error)[]) {
  const fetcher = vi.fn<typeof fetch>()
  for (const res of responses) {
    if (res instanceof Error) fetcher.mockRejectedValueOnce(res)
    else fetcher.mockResolvedValueOnce(res)
  }
  return { fetcher, client: new ApiClient(fetcher) }
}

function headersOf(fetcher: ReturnType<typeof vi.fn<typeof fetch>>, call = 0) {
  return fetcher.mock.calls[call][1]?.headers as Record<string, string>
}

async function caught(promise: Promise<unknown>): Promise<ApiError> {
  try {
    await promise
  } catch (err) {
    if (err instanceof ApiError) return err
    throw err
  }
  throw new Error('request seharusnya gagal')
}

describe('buildUrl dan path', () => {
  it('menambahkan prefix dan query, melewati nilai kosong', () => {
    expect(buildUrl('/databases', { page: 2, limit: 50, hide_rollback: true, status: undefined, q: '' })).toBe('/api/v1/databases?page=2&limit=50&hide_rollback=true')
  })

  it('menolak path tanpa garis miring awal', () => {
    expect(() => buildUrl('databases')).toThrow()
  })

  it('meng-encode setiap segmen', () => {
    expect(path`/databases/${'db 1'}/tables/${'public'}/${'a/b?c'}`).toBe('/databases/db%201/tables/public/a%2Fb%3Fc')
  })
})

describe('ApiClient', () => {
  it('GET mengembalikan JSON dan mengirim cookie same-origin tanpa CSRF', async () => {
    const { fetcher, client } = setup(json(200, { items: [1] }))
    client.configure({ csrfToken: 'tok' })
    await expect(client.get('/databases')).resolves.toEqual({ items: [1] })
    const init = fetcher.mock.calls[0][1]
    expect(fetcher.mock.calls[0][0]).toBe('/api/v1/databases')
    expect(init?.method).toBe('GET')
    expect(init?.credentials).toBe('same-origin')
    expect(headersOf(fetcher)['X-CSRF-Token']).toBeUndefined()
  })

  it('mutasi mengirim body JSON dan header X-CSRF-Token', async () => {
    const { fetcher, client } = setup(json(202, { job_id: 'j1', status: 'queued' }))
    client.configure({ csrfToken: 'tok' })
    await expect(client.post('/x', { a: 1 })).resolves.toEqual({ job_id: 'j1', status: 'queued' })
    expect(headersOf(fetcher)['X-CSRF-Token']).toBe('tok')
    expect(headersOf(fetcher)['Content-Type']).toBe('application/json')
    expect(fetcher.mock.calls[0][1]?.body).toBe('{"a":1}')
  })

  it('204 menghasilkan undefined', async () => {
    const { client } = setup(new Response(null, { status: 204 }))
    await expect(client.delete('/x')).resolves.toBeUndefined()
  })

  it('membaca envelope error termasuk fields validasi', async () => {
    const { client } = setup(json(422, errorBody('validation_failed', 'Isian tidak valid', { fields: { title: 'Wajib diisi' } })))
    const err = await caught(client.post('/x', {}))
    expect(err.status).toBe(422)
    expect(err.code).toBe('validation_failed')
    expect(err.message).toBe('Isian tidak valid')
    expect(err.requestId).toBe('req-1')
    expect(err.fields).toEqual({ title: 'Wajib diisi' })
  })

  it('menormalkan fields berbentuk daftar', async () => {
    const { client } = setup(json(422, errorBody('validation_failed', '', { fields: [{ field: 'down_sql', message: 'Wajib' }] })))
    expect((await caught(client.post('/x', {}))).fields).toEqual({ down_sql: 'Wajib' })
  })

  it('409 menjadi conflict', async () => {
    const { client } = setup(json(409, errorBody('conflict', 'Revision berubah')))
    const err = await caught(client.patch('/x', {}))
    expect(err.code).toBe('conflict')
  })

  it('401 memanggil onUnauthenticated', async () => {
    const onUnauthenticated = vi.fn()
    const { client } = setup(json(401, errorBody('unauthenticated')))
    client.configure({ onUnauthenticated })
    expect((await caught(client.get('/x'))).code).toBe('unauthenticated')
    expect(onUnauthenticated).toHaveBeenCalledOnce()
  })

  it('401 tidak memicu onUnauthenticated jika dinonaktifkan', async () => {
    const onUnauthenticated = vi.fn()
    const { client } = setup(json(401, errorBody('unauthenticated')))
    client.configure({ onUnauthenticated })
    await caught(client.get('/auth/me', { handleUnauthenticated: false }))
    expect(onUnauthenticated).not.toHaveBeenCalled()
  })

  it('403 forbidden tidak mencoba ulang', async () => {
    const refreshCsrf = vi.fn()
    const { fetcher, client } = setup(json(403, errorBody('forbidden')))
    client.configure({ csrfToken: 'tok', refreshCsrf })
    expect((await caught(client.post('/x'))).code).toBe('forbidden')
    expect(refreshCsrf).not.toHaveBeenCalled()
    expect(fetcher).toHaveBeenCalledOnce()
  })

  it('csrf_failed mengambil token baru lalu mengulang sekali', async () => {
    const { fetcher, client } = setup(json(403, errorBody('csrf_failed')), json(200, { ok: true }))
    client.configure({ csrfToken: 'lama', refreshCsrf: async () => 'baru' })
    await expect(client.post('/x', {})).resolves.toEqual({ ok: true })
    expect(fetcher).toHaveBeenCalledTimes(2)
    expect(headersOf(fetcher, 0)['X-CSRF-Token']).toBe('lama')
    expect(headersOf(fetcher, 1)['X-CSRF-Token']).toBe('baru')
  })

  it('csrf_failed berulang tidak membuat loop', async () => {
    const refreshCsrf = vi.fn(async () => 'baru')
    const { fetcher, client } = setup(json(403, errorBody('csrf_failed')), json(403, errorBody('csrf_failed')))
    client.configure({ csrfToken: 'lama', refreshCsrf })
    expect((await caught(client.post('/x'))).code).toBe('csrf_failed')
    expect(refreshCsrf).toHaveBeenCalledOnce()
    expect(fetcher).toHaveBeenCalledTimes(2)
  })

  it('csrf_failed tanpa sesi aktif dianggap keluar', async () => {
    const onUnauthenticated = vi.fn()
    const { fetcher, client } = setup(json(403, errorBody('csrf_failed')))
    client.configure({ csrfToken: 'lama', refreshCsrf: async () => null, onUnauthenticated })
    await caught(client.post('/x'))
    expect(onUnauthenticated).toHaveBeenCalledOnce()
    expect(fetcher).toHaveBeenCalledOnce()
  })

  it('429 membaca Retry-After', async () => {
    const { client } = setup(json(429, errorBody('rate_limited'), { 'Retry-After': '30' }))
    const err = await caught(client.post('/auth/login', {}))
    expect(err.code).toBe('rate_limited')
    expect(err.retryAfter).toBe(30)
  })

  it('error tanpa envelope JSON tetap menjadi ApiError', async () => {
    const { client } = setup(new Response('<html>Bad Gateway</html>', { status: 502, headers: { 'Content-Type': 'text/html' } }))
    const err = await caught(client.get('/x'))
    expect(err.status).toBe(502)
    expect(err.code).toBe('internal')
    expect(err.message).toBe('')
  })

  it('kode error yang tidak dikenal memakai kode dari status', async () => {
    const { client } = setup(json(404, errorBody('sesuatu_baru')))
    expect((await caught(client.get('/x'))).code).toBe('not_found')
  })

  it('gagal jaringan menjadi kode network', async () => {
    const { client } = setup(new TypeError('Failed to fetch'))
    const err = await caught(client.get('/x'))
    expect(err.status).toBe(0)
    expect(err.code).toBe('network')
  })

  it('pembatalan dilempar ulang apa adanya', async () => {
    const controller = new AbortController()
    controller.abort()
    const { client } = setup(new DOMException('aborted', 'AbortError'))
    await expect(client.get('/x', { signal: controller.signal })).rejects.toBeInstanceOf(DOMException)
  })
})
