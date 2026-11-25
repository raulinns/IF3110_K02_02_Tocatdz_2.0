import type { Schemas } from '../api/types.ts'

type Request = Schemas['ChangeRequest']
type Approval = Schemas['Approval']

// Approval yang masih menunggu keputusan user ini, dan pengaju tidak pernah bisa memutuskan permintaannya sendiri
export function pendingApprovalsOf(request: Request, userId: string): Approval[] {
  if (request.status !== 'pending_review' || request.requested_by === userId) return []
  return request.approvals.filter((approval) => approval.leader_user_id === userId && approval.decision === null)
}

export const awaitsDecisionFrom = (request: Request, userId: string) => pendingApprovalsOf(request, userId).length > 0

export const approvedCount = (request: Request) => request.approvals.filter((approval) => approval.decision === 'approve').length

// SQL yang dijalankan permintaan: up untuk menerapkan dan melanjutkan, down untuk membatalkan
export function sqlDirection(kind: Schemas['ChangeKind']): Schemas['Direction'] {
  return kind === 'rollback' || kind === 'revert_batches' ? 'down' : 'up'
}
