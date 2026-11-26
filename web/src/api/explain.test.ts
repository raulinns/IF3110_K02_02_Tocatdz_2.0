import { describe, expect, it } from 'vitest'
import { ApiError, explainConflict } from './errors.ts'

const failure = (init: Partial<ConstructorParameters<typeof ApiError>[0]> = {}) => new ApiError({ status: 409, code: 'conflict', message: 'Data atau status berubah', requestId: 'r1', ...init })
const run = (error: unknown) => Promise.reject(error).catch(explainConflict('Penjelasan'))

describe('explainConflict', () => {
  it('mengganti pesan konflik tanpa reason dan mempertahankan status dan ID permintaan', async () => {
    const result = await run(failure()).catch((error: ApiError) => error)
    expect(result).toBeInstanceOf(ApiError)
    expect(result).toMatchObject({ message: 'Penjelasan', status: 409, code: 'conflict', requestId: 'r1' })
  })

  it('tidak menyentuh konflik yang membawa reason dari server', async () => {
    const result = await run(failure({ reason: 'revision_mismatch' })).catch((error: ApiError) => error)
    expect(result.message).toBe('Data atau status berubah')
  })

  it('tidak menyentuh galat lain', async () => {
    for (const error of [failure({ status: 403, code: 'forbidden', message: 'Akses ditolak' }), failure({ status: 422, code: 'validation_failed' }), new Error('bug')]) {
      const result = await run(error).catch((caught: Error) => caught)
      expect(result).toBe(error)
    }
  })
})
