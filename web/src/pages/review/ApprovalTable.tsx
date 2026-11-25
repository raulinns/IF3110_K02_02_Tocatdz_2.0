import type { Schemas } from '../../api/types.ts'
import { formatDateTime } from '../../lib/format.ts'
import { accessLabel, decisionLabel } from '../../lib/labels.ts'

interface ApprovalTableProps {
  request: Schemas['ChangeRequest']
  userId: string
  name: (id: string | null) => string
  // Hanya diberikan saat pengguna boleh memutuskan untuk sebuah group
  onDecide?: (approval: Schemas['Approval']) => void
}

const tone = { approve: 'status status-applied', reject: 'status status-failed' } as const

export function ApprovalTable({ request, userId, name, onDecide }: ApprovalTableProps) {
  const own = request.requested_by === userId
  return (
    <div className="table-responsive">
      <table className="table table-hover mb-0" aria-label="Approver per group">
        <thead>
          <tr>
            <th scope="col">Group</th>
            <th scope="col">Leader</th>
            <th scope="col">Keputusan</th>
            <th scope="col">Waktu</th>
            <th scope="col">Catatan</th>
            <th scope="col" className="text-end">
              Tindakan
            </th>
          </tr>
        </thead>
        <tbody>
          {request.approvals.map((approval) => {
            const mine = approval.leader_user_id === userId
            const open = request.status === 'pending_review' && approval.decision === null
            return (
              <tr key={approval.group_id}>
                <td>
                  <i className="fas fa-users text-muted" aria-hidden="true" /> {approval.group_name}
                  <div className="small text-muted">{accessLabel(approval.access)}</div>
                </td>
                <td className="mono">{name(approval.leader_user_id)}</td>
                <td>
                  <span className={`badge ${approval.decision ? tone[approval.decision] : 'status'}`}>{decisionLabel(approval.decision)}</span>
                  {approval.actor_id && approval.actor_id !== approval.leader_user_id && <div className="small text-muted">oleh {name(approval.actor_id)}</div>}
                </td>
                <td className="text-nowrap">{approval.decided_at ? <time dateTime={approval.decided_at}>{formatDateTime(approval.decided_at)}</time> : '-'}</td>
                <td className="diff-cell">{approval.note || '-'}</td>
                <td className="text-end">
                  {open && mine && onDecide && (
                    <button type="button" className="btn btn-sm btn-primary" disabled={own} title={own ? 'Pengaju tidak dapat memutuskan permintaannya sendiri' : undefined} onClick={() => onDecide(approval)}>
                      <i className="fas fa-gavel" aria-hidden="true" />
                      Putuskan
                    </button>
                  )}
                  {open && mine && own && <div className="small text-muted">Anda pengaju</div>}
                </td>
              </tr>
            )
          })}
        </tbody>
      </table>
    </div>
  )
}
