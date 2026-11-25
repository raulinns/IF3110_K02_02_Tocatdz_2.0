import { describe, expect, it } from 'vitest'
import { elapsed, formatDuration } from './duration.ts'

describe('formatDuration', () => {
  it('memilih satuan menurut besarnya', () => {
    expect(formatDuration(0)).toBe('0 ms')
    expect(formatDuration(850)).toBe('850 ms')
    expect(formatDuration(1500)).toBe('1.5 dtk')
    expect(formatDuration(59_000)).toBe('59 dtk')
    expect(formatDuration(65_000)).toBe('1 mnt 5 dtk')
    expect(formatDuration(3_660_000)).toBe('1 jam 1 mnt')
  })

  it('nilai yang tidak masuk akal menjadi garis', () => {
    expect(formatDuration(-1)).toBe('-')
    expect(formatDuration(Number.NaN)).toBe('-')
  })
})

describe('elapsed', () => {
  const now = Date.parse('2026-11-10T10:00:10Z')

  it('menghitung selisih dua waktu', () => {
    expect(elapsed('2026-11-10T10:00:00Z', '2026-11-10T10:00:04Z', now)).toBe(4000)
  })

  it('tanpa waktu akhir dihitung sampai sekarang', () => {
    expect(elapsed('2026-11-10T10:00:00Z', null, now)).toBe(10_000)
  })

  it('tanpa waktu mulai atau waktu tidak valid menghasilkan null', () => {
    expect(elapsed(null, null, now)).toBeNull()
    expect(elapsed('bukan-waktu', null, now)).toBeNull()
  })

  it('selisih tidak pernah negatif walau jam peramban tertinggal', () => {
    expect(elapsed('2026-11-10T10:00:20Z', null, now)).toBe(0)
  })
})
