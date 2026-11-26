import { vi } from 'vitest'

interface Behavior {
  delay?: number
  failures?: number
}

// Memperlambat atau menggagalkan sejumlah request tertentu tanpa mengubah request lainnya
export function interceptFetch(match: string, { delay = 0, failures = 0 }: Behavior = {}) {
  const original = globalThis.fetch
  let remaining = failures
  return vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    if (String(input).includes(match)) {
      if (delay > 0) await new Promise((resolve) => setTimeout(resolve, delay))
      if (remaining > 0) {
        remaining -= 1
        throw new TypeError('Failed to fetch')
      }
    }
    return original(input, init)
  })
}

// Pesan umum per kode galat di api/errors.go, karena server sungguhan tidak mengirim pesan khusus per kasus
const genericMessages: Record<string, string> = { conflict: 'Data atau status berubah, silakan muat ulang', forbidden: 'Akses ditolak', validation_failed: 'Input tidak valid' }

// Meniru server Go sungguhan: respons galat dari mock ditulis ulang tanpa reason dan dengan pesan umum
export function realServerErrors() {
  const original = globalThis.fetch
  return vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    const response = await original(input, init)
    if (response.ok) return response
    const body = (await response.clone().json().catch(() => null)) as { error?: Record<string, unknown> } | null
    if (!body?.error) return response
    const error = { ...body.error }
    delete error.reason
    if (typeof error.code === 'string' && genericMessages[error.code]) error.message = genericMessages[error.code]
    return new Response(JSON.stringify({ error }), { status: response.status, headers: response.headers })
  })
}

export const callsTo = (spy: { mock: { calls: unknown[][] } }, match: string) => spy.mock.calls.filter((call) => String(call[0]).includes(match)).length
