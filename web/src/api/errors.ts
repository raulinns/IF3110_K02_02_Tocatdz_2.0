// Kode error yang didefinisikan kontrak API
const serverCodes = [
  'bad_request',
  'unauthenticated',
  'forbidden',
  'not_found',
  'method_not_allowed',
  'validation_failed',
  'conflict',
  'csrf_failed',
  'rate_limited',
  'payload_too_large',
  'unsupported_media_type',
  'result_too_large',
  'unavailable',
  'internal',
] as const

export type ApiErrorCode = (typeof serverCodes)[number] | 'network'

const knownCodes = new Set<string>(serverCodes)

interface ApiErrorInit {
  status: number
  code: ApiErrorCode
  message: string
  requestId?: string
  reason?: string
  fields?: Record<string, string[]>
  retryAfter?: number
}

// Status 0 berarti request tidak sampai ke server (jaringan putus, server mati)
export class ApiError extends Error {
  readonly status: number
  readonly code: ApiErrorCode
  readonly requestId?: string
  readonly reason?: string
  readonly fields: Record<string, string[]>
  readonly retryAfter?: number

  constructor(init: ApiErrorInit) {
    super(init.message)
    this.name = 'ApiError'
    this.status = init.status
    this.code = init.code
    this.requestId = init.requestId
    this.reason = init.reason
    this.fields = init.fields ?? {}
    this.retryAfter = init.retryAfter
  }
}

export function isApiError(err: unknown): err is ApiError {
  return err instanceof ApiError
}

export function codeForStatus(status: number): ApiErrorCode {
  switch (status) {
    case 400:
      return 'bad_request'
    case 401:
      return 'unauthenticated'
    case 403:
      return 'forbidden'
    case 404:
      return 'not_found'
    case 405:
      return 'method_not_allowed'
    case 409:
      return 'conflict'
    case 413:
      return 'payload_too_large'
    case 415:
      return 'unsupported_media_type'
    case 422:
      return 'validation_failed'
    case 429:
      return 'rate_limited'
    case 503:
      return 'unavailable'
    default:
      return 'internal'
  }
}

// Kunci berupa path field dan nilainya daftar pesan, bentuk lain diabaikan
function normalizeFields(raw: unknown): Record<string, string[]> {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return {}
  return Object.fromEntries(
    Object.entries(raw).flatMap(([key, value]): [string, string[]][] => {
      const messages = Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : []
      return messages.length > 0 ? [[key, messages]] : []
    }),
  )
}

function parseRetryAfter(header: string | null): number | undefined {
  if (!header) return undefined
  const seconds = Number(header)
  if (Number.isFinite(seconds) && seconds >= 0) return Math.ceil(seconds)
  const date = Date.parse(header)
  if (Number.isNaN(date)) return undefined
  return Math.max(0, Math.ceil((date - Date.now()) / 1000))
}

// Body yang rusak atau bukan envelope tetap menghasilkan ApiError dengan kode dari status
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
    reason: typeof body?.reason === 'string' ? body.reason : undefined,
    fields: normalizeFields(body?.fields),
    retryAfter: parseRetryAfter(res.headers.get('Retry-After')),
  })
}
