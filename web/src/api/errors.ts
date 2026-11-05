export type ApiErrorCode =
  | 'unauthenticated'
  | 'forbidden'
  | 'not_found'
  | 'validation_failed'
  | 'conflict'
  | 'csrf_failed'
  | 'rate_limited'
  | 'internal'
  | 'network'

const knownCodes = new Set<string>(['unauthenticated', 'forbidden', 'not_found', 'validation_failed', 'conflict', 'csrf_failed', 'rate_limited', 'internal'])

interface ApiErrorInit {
  status: number
  code: ApiErrorCode
  message: string
  requestId?: string
  fields?: Record<string, string>
  retryAfter?: number
}

// Status 0 berarti request tidak sampai ke server (jaringan putus, server mati).
export class ApiError extends Error {
  readonly status: number
  readonly code: ApiErrorCode
  readonly requestId?: string
  readonly fields: Record<string, string>
  readonly retryAfter?: number

  constructor(init: ApiErrorInit) {
    super(init.message)
    this.name = 'ApiError'
    this.status = init.status
    this.code = init.code
    this.requestId = init.requestId
    this.fields = init.fields ?? {}
    this.retryAfter = init.retryAfter
  }
}

export function isApiError(err: unknown): err is ApiError {
  return err instanceof ApiError
}

export function codeForStatus(status: number): ApiErrorCode {
  switch (status) {
    case 401:
      return 'unauthenticated'
    case 403:
      return 'forbidden'
    case 404:
      return 'not_found'
    case 409:
      return 'conflict'
    case 422:
      return 'validation_failed'
    case 429:
      return 'rate_limited'
    default:
      return 'internal'
  }
}

// fields boleh berupa map {field: pesan} atau daftar [{field, message}]; keduanya dinormalkan ke map.
function normalizeFields(raw: unknown): Record<string, string> {
  const fields: Record<string, string> = {}
  if (Array.isArray(raw)) {
    for (const item of raw) {
      if (item && typeof item.field === 'string' && typeof item.message === 'string') fields[item.field] = item.message
    }
  } else if (raw && typeof raw === 'object') {
    for (const [key, value] of Object.entries(raw)) {
      if (typeof value === 'string') fields[key] = value
    }
  }
  return fields
}

function parseRetryAfter(header: string | null): number | undefined {
  if (!header) return undefined
  const seconds = Number(header)
  if (Number.isFinite(seconds) && seconds >= 0) return Math.ceil(seconds)
  const date = Date.parse(header)
  if (Number.isNaN(date)) return undefined
  return Math.max(0, Math.ceil((date - Date.now()) / 1000))
}

// Membaca envelope {"error": {code, message, request_id, fields}}; body yang rusak tetap menghasilkan ApiError.
export async function toApiError(res: Response): Promise<ApiError> {
  let envelope: unknown
  try {
    envelope = await res.json()
  } catch {
    envelope = undefined
  }
  const body = envelope && typeof envelope === 'object' ? (envelope as { error?: Record<string, unknown> }).error : undefined
  const code = typeof body?.code === 'string' && knownCodes.has(body.code) ? (body.code as ApiErrorCode) : codeForStatus(res.status)
  return new ApiError({
    status: res.status,
    code,
    message: typeof body?.message === 'string' ? body.message : '',
    requestId: typeof body?.request_id === 'string' ? body.request_id : res.headers.get('X-Request-ID') ?? undefined,
    fields: normalizeFields(body?.fields),
    retryAfter: parseRetryAfter(res.headers.get('Retry-After')),
  })
}
