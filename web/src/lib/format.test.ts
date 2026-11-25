import { describe, expect, it } from 'vitest'
import { formatDateTime, formatMetric } from './format.ts'

describe('format', () => {
  it('waktu RFC 3339 diformat dan nilai tidak valid dikembalikan apa adanya', () => {
    expect(formatDateTime('2026-11-10T10:00:00Z')).toMatch(/2026/)
    expect(formatDateTime('bukan-waktu')).toBe('bukan-waktu')
  })

  it('metrik dibulatkan tiga desimal dan yang tidak ada menjadi garis', () => {
    expect(formatMetric(1.23456)).toBe('1.235')
    expect(formatMetric(10)).toBe('10')
    expect(formatMetric(0)).toBe('0')
    expect(formatMetric(null)).toBe('-')
    expect(formatMetric(undefined)).toBe('-')
  })
})
