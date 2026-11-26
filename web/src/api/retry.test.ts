import { describe, expect, it } from 'vitest'
import { ApiError } from './errors.ts'
import { shouldRetryQuery } from './retry.ts'

const failure = (status: number, code: ConstructorParameters<typeof ApiError>[0]['code']) => new ApiError({ status, code, message: 'x' })

describe('kebijakan ulang pembacaan', () => {
  it('gangguan jaringan dan error server diulang dua kali lalu berhenti', () => {
    for (const error of [failure(0, 'network'), failure(500, 'internal'), failure(503, 'unavailable')]) {
      expect(shouldRetryQuery(0, error)).toBe(true)
      expect(shouldRetryQuery(1, error)).toBe(true)
      expect(shouldRetryQuery(2, error)).toBe(false)
    }
  })

  it('konflik pada pembacaan diulang satu kali karena bisa berasal dari sesi yang sedang diperbarui', () => {
    const conflict = failure(409, 'conflict')
    expect(shouldRetryQuery(0, conflict)).toBe(true)
    expect(shouldRetryQuery(1, conflict)).toBe(false)
  })

  it('galat 4xx lain langsung ditampilkan tanpa diulang', () => {
    for (const error of [failure(400, 'bad_request'), failure(401, 'unauthenticated'), failure(403, 'forbidden'), failure(404, 'not_found'), failure(422, 'validation_failed'), failure(429, 'rate_limited')]) {
      expect(shouldRetryQuery(0, error)).toBe(false)
    }
  })

  it('kesalahan yang bukan galat API tidak diulang', () => {
    expect(shouldRetryQuery(0, new Error('bug'))).toBe(false)
    expect(shouldRetryQuery(0, null)).toBe(false)
  })
})
