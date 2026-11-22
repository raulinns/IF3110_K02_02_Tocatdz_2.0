import { describe, expect, it } from 'vitest'
import { ApiError, type ApiErrorCode } from './errors.ts'
import { errorMessage, fieldError, fieldErrors } from './messages.ts'

function error(code: ApiErrorCode, message = '', extra: Partial<ConstructorParameters<typeof ApiError>[0]> = {}) {
  return new ApiError({ status: 400, code, message, ...extra })
}

const codes: ApiErrorCode[] = [
  'network',
  'bad_request',
  'unauthenticated',
  'forbidden',
  'csrf_failed',
  'not_found',
  'method_not_allowed',
  'conflict',
  'validation_failed',
  'payload_too_large',
  'unsupported_media_type',
  'result_too_large',
  'rate_limited',
  'unavailable',
]

describe('errorMessage', () => {
  it('memakai pesan server untuk konflik dan validasi', () => {
    expect(errorMessage(error('conflict', 'Revision berubah'))).toBe('Revision berubah')
    expect(errorMessage(error('validation_failed', 'SQL down wajib diisi'))).toBe('SQL down wajib diisi')
  })

  it('tidak menampilkan pesan server untuk error internal', () => {
    expect(errorMessage(error('internal', 'pq: connection refused at 10.0.0.1'))).not.toContain('10.0.0.1')
  })

  it('menyebut waktu tunggu rate limit', () => {
    expect(errorMessage(error('rate_limited', '', { retryAfter: 12 }))).toContain('12 detik')
  })

  it('error yang bukan ApiError tetap punya pesan', () => {
    expect(errorMessage(new Error('x'))).toBeTruthy()
  })

  it.each(codes)('kode %s punya pesan sendiri', (code) => {
    const message = errorMessage(error(code, 'pesan dari server'))
    expect(message).toBeTruthy()
    expect(message).not.toBe(errorMessage(error('internal')))
  })

  it('reason mengganti pesan server yang generik', () => {
    const server = 'Data atau status berubah, silakan muat ulang'
    const message = errorMessage(error('conflict', server, { reason: 'revision_mismatch' }))
    expect(message).not.toBe(server)
    expect(message).toContain('revisi')
  })

  it('reason yang tidak dikenal kembali ke pesan server', () => {
    expect(errorMessage(error('conflict', 'Pesan khusus', { reason: 'tidak_dikenal' }))).toBe('Pesan khusus')
  })

  it('reason bernama properti bawaan objek tidak dianggap dikenal', () => {
    expect(errorMessage(error('conflict', '', { reason: 'constructor' }))).toBe(errorMessage(error('conflict')))
  })
})

describe('fieldErrors', () => {
  it('mengambil semua pesan untuk satu field', () => {
    const err = error('validation_failed', '', { fields: { up_sql: ['Terlalu panjang', 'Ada beberapa statement'] } })
    expect(fieldErrors(err, 'up_sql')).toEqual(['Terlalu panjang', 'Ada beberapa statement'])
  })

  it('mengembalikan daftar kosong untuk field tanpa error atau error non-API', () => {
    const err = error('validation_failed', '', { fields: { title: ['Wajib diisi'] } })
    expect(fieldErrors(err, 'down_sql')).toEqual([])
    expect(fieldErrors(err, 'constructor')).toEqual([])
    expect(fieldErrors(new Error('x'), 'title')).toEqual([])
  })
})

describe('fieldError', () => {
  it('mengambil pesan pertama per field', () => {
    const err = error('validation_failed', '', { fields: { title: ['Wajib diisi', 'Terlalu pendek'] } })
    expect(fieldError(err, 'title')).toBe('Wajib diisi')
    expect(fieldError(err, 'up_sql')).toBeUndefined()
    expect(fieldError(new Error('x'), 'title')).toBeUndefined()
  })
})
