import { describe, expect, it } from 'vitest'
import type { Schemas } from '../api/types.ts'
import { changeKindLabel, decisionLabel, definitionStateLabel, modeLabel, runStatusLabel, runStatusTone, type RunStatus } from './labels.ts'

const everyStatus: RunStatus[] = ['pending_review', 'approved', 'rejected', 'queued', 'running', 'succeeded', 'failed', 'partial', 'uncertain', 'partially_reverted']

describe('label workflow', () => {
  it('setiap status punya label yang berbeda dan tidak kosong', () => {
    const labels = everyStatus.map(runStatusLabel)
    expect(labels.every((label) => label.length > 0)).toBe(true)
    expect(new Set(labels).size).toBe(everyStatus.length)
  })

  it('nada warna mengikuti hasilnya', () => {
    expect(runStatusTone('succeeded')).toBe('success')
    expect(runStatusTone('approved')).toBe('success')
    expect(runStatusTone('failed')).toBe('danger')
    expect(runStatusTone('rejected')).toBe('danger')
    for (const status of ['pending_review', 'queued', 'running'] as const) expect(runStatusTone(status)).toBe('pending')
    for (const status of ['partial', 'uncertain', 'partially_reverted'] as const) expect(runStatusTone(status)).toBe('warning')
  })

  it('jenis permintaan dan keputusan punya label', () => {
    const kinds: Schemas['ChangeKind'][] = ['apply', 'rollback', 'forward', 'resume', 'revert_batches']
    expect(kinds.map(changeKindLabel)).toEqual(['Terapkan', 'Rollback', 'Forward', 'Resume', 'Revert batch'])
    expect([decisionLabel('approve'), decisionLabel('reject'), decisionLabel(null)]).toEqual(['Disetujui', 'Ditolak', 'Menunggu'])
  })

  it('label mode dan status definisi', () => {
    expect(modeLabel('batch')).toBe('Batch')
    expect(modeLabel('normal')).toBe('Normal')
    expect(definitionStateLabel('frozen')).toBe('Dibekukan')
    expect(definitionStateLabel('draft')).toBe('Draft')
  })
})
