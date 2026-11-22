import { HttpResponse } from 'msw/http'
import type { Schemas } from '../api/types.ts'
import { getState, type MockSession, type MockUser } from './state.ts'

export const apiPrefix = '/api/v1'

// Awalan wildcard membuat handler cocok untuk origin apa pun, baik di browser maupun di Node
export const route = (path: string) => `*${apiPrefix}${path}`

export const pagingKeys = ['page', 'limit'] as const

const latencyMs = import.meta.env.MODE === 'mock' ? 200 : 0
const uuidPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/
const canonicalInteger = /^[1-9][0-9]{0,6}$/
const defaultLimit = 50
const maxLimit = 200
const maxOffset = 1_000_000
let requestCounter = 0

interface FailureExtra {
  reason?: string
  fields?: Record<string, string[]>
  headers?: Record<string, string>
}

export class ApiFailure extends Error {
  readonly status: number
  readonly code: Schemas['ErrorCode']
  readonly extra: FailureExtra

  constructor(status: number, code: Schemas['ErrorCode'], message: string, extra: FailureExtra = {}) {
    super(message)
    this.status = status
    this.code = code
    this.extra = extra
  }
}

export const badRequest = () => new ApiFailure(400, 'bad_request', 'Permintaan JSON, path, atau query tidak valid')
export const unauthenticated = (message = 'Silakan login kembali') => new ApiFailure(401, 'unauthenticated', message)
export const forbidden = () => new ApiFailure(403, 'forbidden', 'Akses ditolak')
export const notFound = () => new ApiFailure(404, 'not_found', 'Data tidak ditemukan')
export const conflict = (message: string, reason?: string) => new ApiFailure(409, 'conflict', message, { reason })

function baseHeaders(): Record<string, string> {
  requestCounter += 1
  return { 'Cache-Control': 'no-store', 'X-Request-ID': `mock-${String(requestCounter).padStart(6, '0')}` }
}

export function ok(body: unknown, status = 200, headers: Record<string, string> = {}): Response {
  return HttpResponse.json(body as Record<string, unknown>, { status, headers: { ...baseHeaders(), ...headers } })
}

export function noContent(): Response {
  return new HttpResponse(null, { status: 204, headers: baseHeaders() })
}

function failureResponse(failure: ApiFailure): Response {
  const headers = baseHeaders()
  const { reason, fields, headers: extraHeaders } = failure.extra
  const error = { code: failure.code, message: failure.message, request_id: headers['X-Request-ID'], ...(reason ? { reason } : {}), ...(fields ? { fields } : {}) }
  return HttpResponse.json({ error }, { status: failure.status, headers: { ...headers, ...extraHeaders } })
}

export interface ResolverInfo {
  request: Request
  params: Record<string, string | readonly string[] | undefined>
}

export interface Context {
  request: Request
  url: URL
  params: Record<string, string>
  user: MockUser
  session: MockSession
}

// Semua ApiFailure yang dilempar handler diubah menjadi envelope error seperti backend
async function run(work: () => Response | Promise<Response>): Promise<Response> {
  if (latencyMs > 0) await new Promise((resolve) => setTimeout(resolve, latencyMs))
  try {
    return await work()
  } catch (err) {
    if (err instanceof ApiFailure) return failureResponse(err)
    console.error('[mock] kesalahan tak terduga', err)
    return failureResponse(new ApiFailure(500, 'internal', 'Terjadi kesalahan'))
  }
}

// Handler tanpa sesi, dipakai untuk login
export function open(resolver: (info: ResolverInfo) => Response | Promise<Response>) {
  return (info: ResolverInfo) => run(() => resolver(info))
}

interface SecuredOptions {
  admin?: boolean
  query?: readonly string[]
}

const safeMethods = new Set(['GET', 'HEAD'])

// Urutan pemeriksaan sama dengan backend: sesi, token CSRF, hak akses, lalu query dan isi request
export function secured(resolver: (ctx: Context) => Response | Promise<Response>, options: SecuredOptions = {}) {
  return ({ request, params }: ResolverInfo) =>
    run(() => {
      const state = getState()
      const session = state.session
      const user = session ? state.users.find((candidate) => candidate.id === session.userId && candidate.active) : undefined
      if (!session || !user) throw unauthenticated()
      if (!safeMethods.has(request.method) && request.headers.get('X-CSRF-Token') !== session.csrfToken) {
        throw new ApiFailure(403, 'csrf_failed', 'Permintaan tidak dapat diverifikasi. Muat ulang halaman lalu coba lagi.')
      }
      if (options.admin && !user.platform_admin) throw forbidden()
      const url = new URL(request.url)
      readQuery(url, options.query ?? [])
      return resolver({ request, url, params: pathParams(params), user, session })
    })
}

// Parameter path berakhiran _id harus berupa UUID huruf kecil yang tidak nol
function pathParams(raw: ResolverInfo['params']): Record<string, string> {
  const params: Record<string, string> = {}
  for (const [key, value] of Object.entries(raw)) {
    const text = Array.isArray(value) ? value[0] : value
    if (typeof text !== 'string') throw badRequest()
    if (key.endsWith('_id') && (!uuidPattern.test(text) || /^[0-]+$/.test(text))) throw badRequest()
    params[key] = text
  }
  return params
}

// Nama query yang tidak dikenal atau berulang ditolak
export function readQuery(url: URL, allowed: readonly string[]): URLSearchParams {
  const seen = new Set<string>()
  for (const key of url.searchParams.keys()) {
    if (!allowed.includes(key) || seen.has(key)) throw badRequest()
    seen.add(key)
  }
  return url.searchParams
}

export interface Paging {
  page: number
  limit: number
}

export function readPaging(url: URL): Paging {
  const query = readQuery(url, pagingKeys)
  const read = (key: string, fallback: number) => {
    const raw = query.get(key)
    if (raw === null) return fallback
    if (!canonicalInteger.test(raw)) throw badRequest()
    return Number(raw)
  }
  const page = read('page', 1)
  const limit = read('limit', defaultLimit)
  if (limit > maxLimit || (page - 1) * limit > maxOffset) throw badRequest()
  return { page, limit }
}

export function pageOf<T>(items: T[], { page, limit }: Paging) {
  const start = (page - 1) * limit
  return { items: items.slice(start, start + limit), page, limit, has_next: items.length > start + limit }
}

export function sortBy<T>(...keys: ((item: T) => string)[]): (a: T, b: T) => number {
  return (a, b) => {
    for (const key of keys) {
      const left = key(a)
      const right = key(b)
      if (left !== right) return left < right ? -1 : 1
    }
    return 0
  }
}

type JsonObject = Record<string, unknown>

// Body harus JSON object tunggal dengan Content-Type yang benar dan tanpa field asing
export async function readJson(request: Request, allowed: readonly string[]): Promise<JsonObject> {
  const type = (request.headers.get('Content-Type') ?? '').trim().toLowerCase()
  if (type !== 'application/json' && type !== 'application/json; charset=utf-8') {
    throw new ApiFailure(415, 'unsupported_media_type', 'Gunakan Content-Type application/json dengan UTF-8')
  }
  let parsed: unknown
  try {
    parsed = JSON.parse(await request.text())
  } catch {
    throw badRequest()
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) throw badRequest()
  if (Object.keys(parsed).some((key) => !allowed.includes(key))) throw badRequest()
  return parsed as JsonObject
}

export async function requireNoBody(request: Request) {
  if ((await request.text()).length > 0) throw badRequest()
}

// Field yang tidak dikirim bernilai undefined, sedangkan tipe yang salah adalah bad_request
export function stringField(body: JsonObject, key: string): string | undefined {
  const value = body[key]
  if (value === undefined) return undefined
  if (typeof value !== 'string') throw badRequest()
  return value
}

export function booleanField(body: JsonObject, key: string): boolean | undefined {
  const value = body[key]
  if (value === undefined) return undefined
  if (typeof value !== 'boolean') throw badRequest()
  return value
}

export function utf8Length(text: string): number {
  return new TextEncoder().encode(text).length
}
