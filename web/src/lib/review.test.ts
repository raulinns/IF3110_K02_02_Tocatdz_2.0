import { describe, expect, it } from 'vitest'
import type { Schemas } from '../api/types.ts'
import { approvedCount, awaitsDecisionFrom, pendingApprovalsOf, sqlDirection } from './review.ts'

const approval = (group: string, leader: string, decision: Schemas['Approval']['decision'] = null): Schemas['Approval'] => ({ group_id: group, group_name: group, access: 'view_edit', leader_user_id: leader, decision, actor_id: decision ? leader : null, note: '', decided_at: null })
const request = (patch: Partial<Schemas['ChangeRequest']> = {}): Schemas['ChangeRequest'] => ({
  id: 'r',
  database_id: 'd',
  migration_id: 'm',
  migration_revision: 1,
  checksum: 'c',
  revision: 1,
  kind: 'apply',
  status: 'pending_review',
  requested_by: 'pengaju',
  target_execution_id: null,
  source_job_id: null,
  expected_position_version: 0,
  approvals: [approval('g1', 'leader1'), approval('g2', 'leader2', 'approve')],
  created_at: '2026-11-10T10:00:00Z',
  updated_at: '2026-11-10T10:00:00Z',
  ...patch,
})

describe('pendingApprovalsOf', () => {
  it('hanya approval group yang dipimpin user dan belum diputuskan', () => {
    expect(pendingApprovalsOf(request(), 'leader1').map((item) => item.group_id)).toEqual(['g1'])
    expect(pendingApprovalsOf(request(), 'leader2')).toEqual([])
    expect(pendingApprovalsOf(request(), 'orang-lain')).toEqual([])
  })

  it('pengaju tidak pernah mendapat approval untuk permintaannya sendiri', () => {
    const own = request({ requested_by: 'leader1' })
    expect(pendingApprovalsOf(own, 'leader1')).toEqual([])
    expect(awaitsDecisionFrom(own, 'leader1')).toBe(false)
  })

  it('permintaan yang tidak lagi menunggu review tidak butuh keputusan', () => {
    for (const status of ['approved', 'rejected', 'queued', 'succeeded'] as const) expect(awaitsDecisionFrom(request({ status }), 'leader1')).toBe(false)
    expect(awaitsDecisionFrom(request(), 'leader1')).toBe(true)
  })

  it('seorang leader dari dua group mendapat dua approval', () => {
    const both = request({ approvals: [approval('g1', 'x'), approval('g2', 'x')] })
    expect(pendingApprovalsOf(both, 'x')).toHaveLength(2)
  })
})

describe('approvedCount dan sqlDirection', () => {
  it('menghitung group yang sudah menyetujui', () => {
    expect(approvedCount(request())).toBe(1)
    expect(approvedCount(request({ approvals: [approval('g', 'l', 'reject')] }))).toBe(0)
  })

  it('arah SQL menurut jenis permintaan', () => {
    expect(['apply', 'forward', 'resume'].map((kind) => sqlDirection(kind as Schemas['ChangeKind']))).toEqual(['up', 'up', 'up'])
    expect(['rollback', 'revert_batches'].map((kind) => sqlDirection(kind as Schemas['ChangeKind']))).toEqual(['down', 'down'])
  })
})
