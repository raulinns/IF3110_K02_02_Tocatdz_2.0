import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useNavigate } from 'react-router-dom'
import { historyEntryQuery, retryFailedJob, type HistoryEntry } from '../../api/history.ts'
import { ApiErrorView } from '../../components/ui/ApiErrorView.tsx'
import { BoxSkeleton } from '../../components/ui/Skeleton.tsx'
import type { Schemas } from '../../api/types.ts'
import { ConfirmDialog } from '../../components/ui/ConfirmDialog.tsx'
import { StatusBadge } from '../../components/ui/StatusBadge.tsx'
import { SqlField } from '../../components/ui/SqlField.tsx'
import { changeKindLabel } from '../../lib/labels.ts'
import { formatDateTime } from '../../lib/format.ts'
import { jobPath, requestPath } from '../../lib/paths.ts'
import { ExecutedStatements } from './ExecutedStatements.tsx'
import { ExecutionRow } from './ExecutionRow.tsx'

interface Props {
  db: string; entry: HistoryEntry; title?: string; canEdit: boolean; active: boolean
  expanded: boolean; highlighted: boolean; onToggle: () => void
  numberOf: (id: string) => number | undefined; onJump: (id: string) => void
}

function AttemptDetail({db, entryId}: {db: string; entryId: string}) {
  const query = useQuery(historyEntryQuery(db, entryId))
  if (query.isPending) return <BoxSkeleton rows={3} />
  if (query.isError) return <ApiErrorView error={query.error} onRetry={() => void query.refetch()} />
  const entry = query.data
  return <>
    <p>Revision {entry.migration_revision} · execution <span className="mono">{entry.execution_id}</span>. Posisi aktif tidak berubah tanpa commit yang terbukti.</p>
    {entry.error && <p role="alert">{entry.error.message}{entry.error.sqlstate ? ` (SQLSTATE ${entry.error.sqlstate})` : ''}</p>}
    {entry.snapshot_available && entry.sql_snapshot !== null
      ? <SqlField label={`Salinan SQL ${entry.direction}`} value={entry.sql_snapshot} readOnly rows={5} hint="Snapshot yang disetujui; statement di bawah menunjukkan hasil dispatch. Snapshot bukan bukti commit." />
      : <p className="text-muted">Salinan SQL tidak tersedia untuk attempt ini, atau SQL belum dipilih sebelum revalidation gagal. Data lama tidak direkonstruksi dari asumsi.</p>}
    {entry.change_request_id && <Link className="me-3" to={requestPath(db, entry.change_request_id)}>Lihat permintaan</Link>}
    {entry.retry_of_job_id && <Link to={jobPath(db, entry.retry_of_job_id)}>Percobaan dari job sebelumnya</Link>}
    <ExecutedStatements db={db} executionId={entry.execution_id} historyId={entry.id} />
  </>
}

export function AttemptRow({ db, entry, title, canEdit, ...rest }: Props) {
  const navigate = useNavigate()
  const [confirm, setConfirm] = useState(false)
  if (entry.committed_at !== null && entry.position_version !== null && entry.direction !== null) {
    const execution: Schemas['ExecutionSummary'] = {
      ...entry, id: entry.execution_id, actor_id: entry.actor_id ?? '', status: 'committed',
      direction: entry.direction, committed_at: entry.committed_at, position_version: entry.position_version,
    }
    return <ExecutionRow {...rest} db={db} execution={execution} title={title} attemptNumber={entry.attempt} historyId={entry.id} attemptStatus={entry.status} resolvedStatus={entry.resolved_status} />
  }
  const panel = `attempt-panel-${entry.id}`
  const retry = async () => {
    if (!entry.job_id) return
    const request = await retryFailedJob(db, entry.job_id)
    void navigate(requestPath(db, request.id), { state: { notice: 'Permintaan jalankan ulang dibuat. Semua group harus menyetujui lagi.' } })
  }
  return <>
    <tr id={`attempt-${entry.id}`}>
      <td className="mono">—</td>
      <td className="text-nowrap"><time dateTime={entry.started_at}>{formatDateTime(entry.started_at)}</time></td>
      <td>{title ?? `Migration ${entry.migration_id.slice(0, 8)}`}</td>
      <td>{changeKindLabel(entry.operation)} <span className="small text-muted">{entry.direction ?? 'Belum ada SQL'}</span></td>
      <td>
        <StatusBadge status={entry.status} />
        <div className="small text-muted">Percobaan {entry.attempt}</div>
        {entry.resolved_status !== entry.status && <div className="small">Hasil rekonsiliasi: <StatusBadge status={entry.resolved_status} /></div>}
      </td>
      <td className="mono">{entry.actor}</td>
      <td>{entry.job_id ? <Link to={jobPath(db, entry.job_id)}>Lihat job</Link> : '—'}</td>
      <td className="text-end">
        <button type="button" className="btn btn-sm btn-outline-secondary" aria-expanded={rest.expanded} aria-controls={panel} onClick={rest.onToggle}>SQL</button>
        {canEdit && entry.can_retry && <button type="button" className="btn btn-sm btn-outline-primary ms-2" onClick={() => setConfirm(true)}>Jalankan ulang</button>}
      </td>
    </tr>
    {rest.expanded && <tr><td colSpan={8} id={panel} role="region" aria-label={`Detail percobaan ${entry.attempt}`}>
      <AttemptDetail db={db} entryId={entry.id} />
    </td></tr>}
    {confirm && <ConfirmDialog title="Ajukan jalankan ulang" confirmLabel="Ajukan" onClose={() => setConfirm(false)} onConfirm={retry}>
      SQL immutable yang sama akan diajukan melalui permintaan baru dengan approval seluruh group. Posisi, permission, dan hasil job sumber diperiksa ulang sebelum eksekusi.
    </ConfirmDialog>}
  </>
}
