import { describe, expect, it } from 'vitest'
import { ApiError, type ApiErrorCode } from './errors.ts'
import { errorMessage, fieldError } from './messages.ts'

function error(code: ApiErrorCode, message = '', extra: Partial<ConstructorParameters<typeof ApiError>[0]> = {}) {
  return new ApiError({ status: 400, code, message, ...extra })
}

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
})

describe('fieldError', () => {
  it('mengambil pesan per field', () => {
    const err = error('validation_failed', '', { fields: { title: 'Wajib diisi' } })
    expect(fieldError(err, 'title')).toBe('Wajib diisi')
    expect(fieldError(err, 'up_sql')).toBeUndefined()
    expect(fieldError(new Error('x'), 'title')).toBeUndefined()
  })
})
