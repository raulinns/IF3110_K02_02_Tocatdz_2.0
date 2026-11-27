import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { executionQuery } from '../../api/workflow.ts'
import type { Schemas } from '../../api/types.ts'
import { ApiErrorView } from '../../components/ui/ApiErrorView.tsx'
import { BoxSkeleton } from '../../components/ui/Skeleton.tsx'
import { SqlField } from '../../components/ui/SqlField.tsx'
import { StatusBadge } from '../../components/ui/StatusBadge.tsx'
import { formatDateTime } from '../../lib/format.ts'
import { changeKindLabel } from '../../lib/labels.ts'
import { jobPath, requestPath } from '../../lib/paths.ts'
import { ExecutedStatements } from './ExecutedStatements.tsx'

interface ExecutionRowProps {
  attemptNumber?: number
  historyId?: string
  attemptStatus?: Schemas['JobStatus']
  resolvedStatus?: Schemas['JobStatus']
  db: string
  execution: Schemas['ExecutionSummary']
  title: string | undefined
  active: boolean
  highlighted: boolean
  expanded: boolean
  onToggle: () => void
  // Nomor posisi eksekusi lain pada halaman ini, atau undefined bila ada di halaman lain
  numberOf: (id: string) => number | undefined
  onJump: (id: string) => void
}

function Detail({ db, execution, historyId }: { db: string; execution: Schemas['ExecutionSummary']; historyId?: string }) {
  const query = useQuery(executionQuery(db, execution.id))
  if (query.isPending) return <BoxSkeleton rows={3} />
  if (query.isError) return <ApiErrorView error={query.error} onRetry={() => void query.refetch()} />
  const detail = query.data
  return (
    <>
      <dl className="facts mb-3">
        <dt>Checksum</dt>
        <dd className="mono small">{detail.checksum}</dd>
        <dt>Mulai</dt>
        <dd>
          <time dateTime={detail.started_at}>{formatDateTime(detail.started_at)}</time>
        </dd>
        {detail.change_request_id && (
          <>
            <dt>Permintaan</dt>
            <dd>
              <Link to={requestPath(db, detail.change_request_id)}>Lihat permintaan</Link>
            </dd>
          </>
        )}
        {detail.job_id && (
          <>
            <dt>Job</dt>
            <dd>
              <Link to={jobPath(db, detail.job_id)}>Lihat job</Link>
            </dd>
          </>
        )}
      </dl>
      <SqlField label={`Salinan SQL ${detail.direction}`} value={detail.sql_snapshot} readOnly rows={5} className="mb-0" hint="SQL persis seperti yang disetujui dan dijalankan, dan tidak bisa diubah." />
      <ExecutedStatements db={db} executionId={execution.id} historyId={historyId} />
    </>
  )
}

export function ExecutionRow({ db, execution, title, active, highlighted, expanded, onToggle, numberOf, onJump, attemptNumber, historyId, attemptStatus, resolvedStatus }: ExecutionRowProps) {
  const panel = `execution-panel-${execution.id}`
  const link = (id: string, symbol: string, label: string) => {
    const number = numberOf(id)
    return number === undefined ? (
      <span className="mono small text-muted" title="Ada di halaman lain">
        {label} {id.slice(0, 8)}
      </span>
    ) : (
      <a
        href={`#execution-${id}`}
        className="mono small"
        onClick={(event) => {
          event.preventDefault()
          onJump(id)
        }}
      >
        {symbol} {label} #{number}
      </a>
    )
  }
  return (
    <>
      <tr id={`execution-${execution.id}`} tabIndex={-1} className={highlighted ? 'table-active' : undefined} aria-current={active ? 'true' : undefined}>
        <td className="mono">#{execution.position_version}</td>
        <td className="text-nowrap">
          <time dateTime={execution.committed_at}>{formatDateTime(execution.committed_at)}</time>
        </td>
        <td>
          {title ?? `Migration ${execution.migration_id.slice(0, 8)}`} {active && <span className="badge status status-applied">Aktif</span>}
        </td>
        <td className="text-nowrap">
          <span className="badge badge-soft">{changeKindLabel(execution.operation)}</span> <span className="small text-muted">{execution.direction}</span>
        </td>
        <td><StatusBadge status={attemptStatus ?? 'succeeded'} />{resolvedStatus && attemptStatus !== resolvedStatus && <div className="small">Hasil rekonsiliasi: <StatusBadge status={resolvedStatus} /></div>}{attemptNumber !== undefined && <div className="small text-muted">{attemptNumber === 0 ? 'Riwayat lama' : `Percobaan ${attemptNumber}`}</div>}</td>
        <td className="mono">{execution.actor}</td>
        <td>
          {execution.reverts_execution_id && link(execution.reverts_execution_id, '↩', 'membatalkan')}
          {execution.forward_of_execution_id && link(execution.forward_of_execution_id, '↪', 'forward dari')}
          {!execution.reverts_execution_id && !execution.forward_of_execution_id && '-'}
        </td>
        <td className="text-end">
          <button type="button" className="btn btn-sm btn-outline-secondary" aria-expanded={expanded} aria-controls={panel} onClick={onToggle}>
            <i className={`fas ${expanded ? 'fa-chevron-up' : 'fa-chevron-down'}`} aria-hidden="true" />
            SQL
          </button>
        </td>
      </tr>
      {expanded && (
        <tr>
          <td colSpan={8} id={panel} role="region" aria-label={`Detail eksekusi ${execution.position_version}`}>
            <Detail db={db} execution={execution} historyId={historyId} />
          </td>
        </tr>
      )}
    </>
  )
}
