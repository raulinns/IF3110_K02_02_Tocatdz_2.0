import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useNavigate, useParams } from 'react-router-dom'
import { migrationQuery } from '../../api/migrations.ts'
import { executeRequest, jobsQuery, requestQuery } from '../../api/workflow.ts'
import type { Schemas } from '../../api/types.ts'
import { useSession } from '../../auth/session-context.ts'
import { ApiErrorView } from '../../components/ui/ApiErrorView.tsx'
import { Breadcrumbs } from '../../components/ui/Breadcrumbs.tsx'
import { ConfirmDialog } from '../../components/ui/ConfirmDialog.tsx'
import { Notice } from '../../components/ui/Notice.tsx'
import { BoxSkeleton } from '../../components/ui/Skeleton.tsx'
import { SqlField } from '../../components/ui/SqlField.tsx'
import { StatusBadge } from '../../components/ui/StatusBadge.tsx'
import { formatDateTime } from '../../lib/format.ts'
import { changeKindLabel, modeLabel } from '../../lib/labels.ts'
import { jobPath, migrationPath, reviewPath } from '../../lib/paths.ts'
import { approvedCount, sqlDirection } from '../../lib/review.ts'
import { useDatabase } from '../../lib/useDatabase.ts'
import { useFlashNotice } from '../../lib/useFlashNotice.ts'
import { useUsernames } from '../../lib/useUsernames.ts'
import { ApprovalTable } from './ApprovalTable.tsx'
import { DecideDialog } from './DecideDialog.tsx'

const kindHints: Record<Schemas['ChangeKind'], string> = {
  apply: 'SQL up dari migration ini dijalankan pada database.',
  rollback: 'SQL down dari migration ini dijalankan untuk membatalkan eksekusi sebelumnya.',
  forward: 'SQL up dari migration yang pernah dibatalkan dijalankan kembali.',
  resume: 'Melanjutkan batch yang tersisa dari job yang berhenti di tengah.',
  revert_batches: 'Membatalkan batch yang sudah di-commit oleh job yang berhenti di tengah.',
}

export function ReviewDetailPage() {
  const { db, canEdit } = useDatabase()
  const { requestId = '' } = useParams()
  const client = useQueryClient()
  const navigate = useNavigate()
  const session = useSession()
  const me = session.status === 'authenticated' ? session.session.user.id : ''
  const names = useUsernames()
  const query = useQuery(requestQuery(db, requestId))
  const request = query.data
  const migration = useQuery({ ...migrationQuery(db, request?.migration_id ?? ''), enabled: request !== undefined })
  const queued = request !== undefined && !['pending_review', 'approved', 'rejected'].includes(request.status)
  const jobs = useQuery({ ...jobsQuery(db, 1), enabled: queued })
  const [deciding, setDeciding] = useState<Schemas['Approval'] | null>(null)
  const [executing, setExecuting] = useState(false)
  const [notice, setNotice] = useFlashNotice()

  if (query.isPending) return <BoxSkeleton rows={8} />
  if (query.isError) return <ApiErrorView error={query.error} onRetry={() => void query.refetch()} />
  if (!request) return null

  const refresh = () => Promise.all([client.invalidateQueries({ queryKey: ['databases', db, 'change-requests'] }), client.invalidateQueries({ queryKey: ['databases', db, 'position'] })])
  const direction = sqlDirection(request.kind)
  const definition = migration.data
  const sql = definition ? (direction === 'down' ? definition.down_sql : definition.up_sql) : null
  const batch = definition?.mode === 'batch'
  const job = jobs.data?.items.find((candidate) => candidate.change_request_id === request.id)
  const waiting = request.approvals.length - approvedCount(request)
  const rejected = request.approvals.find((approval) => approval.decision === 'reject')

  const execute = async () => {
    const accepted = await executeRequest(db, request.id, request.revision)
    await refresh()
    void navigate(jobPath(db, accepted.job_id), { state: { notice: 'Permintaan diantrekan. Status job diperbarui otomatis.' } })
  }

  return (
    <>
      <Breadcrumbs items={[{ label: 'Review', to: reviewPath(db) }, { label: definition?.title ?? 'Permintaan' }]} />
      <div className="d-flex flex-wrap align-items-start justify-content-between gap-2 mb-3">
        <div>
          <h2 className="h5 mb-1">{definition?.title ?? 'Permintaan perubahan'}</h2>
          <div className="d-flex flex-wrap align-items-center gap-2 small text-muted">
            <span className="badge badge-soft">{changeKindLabel(request.kind)}</span>
            <StatusBadge status={request.status} />
            {definition && <span className="badge badge-soft">{modeLabel(definition.mode)}</span>}
            <span>
              Revision <span className="mono">{request.revision}</span>
            </span>
            <span title={request.checksum}>
              Checksum <span className="mono">{request.checksum.slice(0, 12)}</span>
            </span>
          </div>
        </div>
        {job && (
          <Link className="btn btn-sm btn-outline-primary" to={jobPath(db, job.id)}>
            <i className="fas fa-gauge-high" aria-hidden="true" />
            Buka monitor job
          </Link>
        )}
      </div>

      {notice && (
        <Notice kind="success" onDismiss={() => setNotice(null)}>
          {notice}
        </Notice>
      )}
      {definition && definition.checksum !== request.checksum && (
        <Notice kind="error">Checksum migration saat ini berbeda dari yang direview. Jangan setujui permintaan ini dan hubungi pengaju.</Notice>
      )}
      {request.status === 'pending_review' && <Notice kind="info">Menunggu keputusan dari {waiting} group lagi. Semua group harus menyetujui sebelum permintaan bisa dijalankan.</Notice>}
      {request.status === 'rejected' && (
        <Notice kind="error">
          Permintaan ditolak{rejected ? ` oleh group ${rejected.group_name}` : ''}.{rejected?.note ? ` Catatan: ${rejected.note}` : ''} Salin migration ke draft baru bila perlu memperbaikinya.
        </Notice>
      )}

      <div className="card">
        <div className="card-header">
          <i className="fas fa-code text-muted" aria-hidden="true" />
          <h2 className="h6 mb-0">SQL yang akan dijalankan</h2>
          <span className="card-header-meta">{direction === 'down' ? 'SQL down' : 'SQL up'}</span>
        </div>
        <div className="card-body">
          <p className="text-muted small">{kindHints[request.kind]}</p>
          {migration.isPending && <BoxSkeleton rows={3} />}
          {migration.isError && <ApiErrorView error={migration.error} onRetry={() => void migration.refetch()} />}
          {definition && (
            <>
              {definition.mode === 'batch' && definition.batch && (
                <dl className="facts mb-3">
                  <dt>Tabel</dt>
                  <dd className="mono">
                    {definition.batch.schema}.{definition.batch.table}
                  </dd>
                  <dt>Kolom kunci</dt>
                  <dd className="mono">{definition.batch.key_column}</dd>
                  <dt>Ukuran batch</dt>
                  <dd className="mono">{definition.batch.batch_size}</dd>
                  <dt>Predikat</dt>
                  <dd className="mono">{definition.batch.scope_where ?? '-'}</dd>
                </dl>
              )}
              {sql ? (
                <SqlField label={batch ? 'Bagian SET' : direction === 'down' ? 'SQL down' : 'SQL up'} value={sql} readOnly rows={6} className="mb-0" />
              ) : (
                <p className="mb-0 text-muted">Migration ini tidak memiliki SQL {direction}.</p>
              )}
              {definition.irreversible && (
                <div className="mt-3">
                  <Notice kind="warning">
                    <strong>Irreversible.</strong> {definition.irreversible_reason}
                  </Notice>
                </div>
              )}
              <div className="mt-3">
                <Link to={migrationPath(db, definition.id)}>Lihat definisi migration</Link>
              </div>
            </>
          )}
        </div>
      </div>

      <div className="card">
        <div className="card-header">
          <i className="fas fa-user-check text-muted" aria-hidden="true" />
          <h2 className="h6 mb-0">Approval</h2>
          <span className="card-header-meta">
            {approvedCount(request)}/{request.approvals.length} group menyetujui
          </span>
        </div>
        <ApprovalTable request={request} userId={me} name={names} onDecide={setDeciding} />
      </div>

      <div className="card">
        <div className="card-header">
          <i className="fas fa-circle-info text-muted" aria-hidden="true" />
          <h2 className="h6 mb-0">Ringkasan</h2>
        </div>
        <div className="card-body">
          <dl className="facts mb-0">
            <dt>Pengaju</dt>
            <dd className="mono">{names(request.requested_by)}</dd>
            <dt>Diajukan</dt>
            <dd>
              <time dateTime={request.created_at}>{formatDateTime(request.created_at)}</time>
            </dd>
            <dt>Terakhir berubah</dt>
            <dd>
              <time dateTime={request.updated_at}>{formatDateTime(request.updated_at)}</time>
            </dd>
          </dl>
        </div>
      </div>

      {request.status === 'approved' && (
        <div className="card">
          <div className="card-header">
            <i className="fas fa-play text-muted" aria-hidden="true" />
            <h2 className="h6 mb-0">Jalankan</h2>
          </div>
          <div className="card-body">
            {canEdit ? (
              <>
                <p className="text-muted small">Semua group sudah menyetujui. Permintaan masuk antrean dan dijalankan oleh worker, sehingga hasilnya dipantau di halaman job.</p>
                <button type="button" className="btn btn-primary" onClick={() => setExecuting(true)}>
                  <i className="fas fa-play" aria-hidden="true" />
                  Jalankan
                </button>
              </>
            ) : (
              <p className="mb-0 text-muted">Hanya pengguna dengan akses ubah pada database ini yang dapat menjalankan permintaan.</p>
            )}
          </div>
        </div>
      )}

      {deciding && (
        <DecideDialog
          db={db}
          request={request}
          approval={deciding}
          onDecided={(decision) => {
            void refresh()
            setNotice(decision === 'approve' ? `Anda menyetujui atas nama group ${deciding.group_name}.` : `Anda menolak atas nama group ${deciding.group_name}.`)
          }}
          onReload={() => void refresh()}
          onClose={() => setDeciding(null)}
        />
      )}
      {executing && (
        <ConfirmDialog title="Jalankan permintaan" confirmLabel="Jalankan" onConfirm={execute} onClose={() => setExecuting(false)}>
          {batch ? 'Migration dijalankan bertahap. Bila berhenti di tengah, batch yang sudah di-commit tetap tersimpan dan perlu dipulihkan dari halaman job.' : 'SQL dijalankan dalam satu transaksi pada database target.'} Pastikan Anda sudah meninjau SQL di atas.
        </ConfirmDialog>
      )}
    </>
  )
}
