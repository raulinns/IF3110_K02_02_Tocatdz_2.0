import { useEffect } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import { migrationQuery } from '../../api/migrations.ts'
import { attemptsQuery, isFinalJob, jobQuery, pollDelay, positionQuery } from '../../api/workflow.ts'
import { ApiErrorView } from '../../components/ui/ApiErrorView.tsx'
import { Breadcrumbs } from '../../components/ui/Breadcrumbs.tsx'
import { Notice } from '../../components/ui/Notice.tsx'
import { BoxSkeleton } from '../../components/ui/Skeleton.tsx'
import { StatusBadge } from '../../components/ui/StatusBadge.tsx'
import { elapsed, formatDuration } from '../../lib/duration.ts'
import { formatDateTime } from '../../lib/format.ts'
import { changeKindLabel, modeLabel } from '../../lib/labels.ts'
import { historyPath, jobPath, jobsPath, migrationPath, requestPath } from '../../lib/paths.ts'
import { useDatabase } from '../../lib/useDatabase.ts'
import { useFlashNotice } from '../../lib/useFlashNotice.ts'
import { useNow } from '../../lib/useNow.ts'
import { AttemptsTable, JobFailureText } from './AttemptsTable.tsx'
import { JobProgress } from './JobProgress.tsx'
import { JobStepper } from './JobStepper.tsx'
import { RecoveryActions } from './RecoveryActions.tsx'

const outcomes = {
  succeeded: 'Job selesai dan perubahan sudah di-commit.',
  failed: 'Job gagal. Tidak ada perubahan dari job ini yang tersimpan.',
  partial: 'Job berhenti di tengah. Sebagian batch sudah di-commit dan perlu dipulihkan.',
  partially_reverted: 'Pembatalan berhenti di tengah. Sebagian batch sudah dibatalkan dan perlu dipulihkan.',
  uncertain: 'Hasil belum pasti. Jangan jalankan ulang SQL ini. Sistem sedang memeriksa hasil transaksi dan halaman ini diperbarui otomatis.',
  queued: 'Job menunggu diambil worker.',
  running: 'Job sedang berjalan.',
} as const

export function JobMonitorPage() {
  const { db, canEdit } = useDatabase()
  const { jobId = '' } = useParams()
  const client = useQueryClient()
  const [flash, setFlash] = useFlashNotice()

  const query = useQuery({ ...jobQuery(jobId), refetchInterval: (state) => pollDelay(state.state.data?.status) })
  const job = query.data
  const status = job?.status
  const final = status !== undefined && isFinalJob(status)
  const recoverable = status === 'partial' || status === 'partially_reverted'
  const attempts = useQuery({ ...attemptsQuery(jobId), enabled: job !== undefined })
  const migration = useQuery({ ...migrationQuery(db, job?.migration_id ?? ''), enabled: job !== undefined })
  const position = useQuery({ ...positionQuery(db), enabled: job !== undefined })
  const now = useNow(job !== undefined && !final)

  // Setiap perubahan status memuat ulang percobaan, dan hasil akhir juga menyegarkan permintaan, posisi, dan riwayat
  useEffect(() => {
    if (!status) return
    void client.invalidateQueries({ queryKey: ['jobs', jobId, 'attempts'] })
    if (isFinalJob(status) || status === 'uncertain') {
      void client.invalidateQueries({ queryKey: ['databases', db, 'change-requests'] })
      void client.invalidateQueries({ queryKey: ['databases', db, 'position'] })
      void client.invalidateQueries({ queryKey: ['databases', db, 'executions'] })
    }
  }, [status, jobId, db, client])

  if (query.isPending) return <BoxSkeleton rows={8} />
  if (query.isError) return <ApiErrorView error={query.error} onRetry={() => void query.refetch()} />
  if (!job) return null

  const waited = elapsed(job.queued_at, job.started_at, now)
  const ran = elapsed(job.started_at, job.finished_at, now)
  const blocker = position.data?.blocked_by_job_id
  const blocking = blocker === job.id
  // Job partial tetap berstatus partial di server setelah dipulihkan, sehingga perlunya pemulihan ditentukan oleh job yang menahan posisi database
  // Bila posisi gagal dimuat, tombol pemulihan tetap ditawarkan karena server sendiri yang menolak pemulihan yang tidak perlu
  const needsRecovery = recoverable && (position.isError || blocking)
  const recovered = recoverable && position.data !== undefined && !blocking
  const polling = pollDelay(job.status) !== false

  return (
    <>
      <Breadcrumbs items={[{ label: 'Monitor', to: jobsPath(db) }, { label: migration.data?.title ?? 'Job' }]} />
      <div className="d-flex flex-wrap align-items-start justify-content-between gap-2 mb-3">
        <div>
          <h2 className="h5 mb-1">{migration.data?.title ?? 'Job'}</h2>
          <div className="d-flex flex-wrap align-items-center gap-2 small text-muted">
            <span className="badge badge-soft">{changeKindLabel(job.kind)}</span>
            <StatusBadge status={job.status} />
            <span className="badge badge-soft">{modeLabel(job.mode)}</span>
            <span>
              Percobaan <span className="mono">{job.attempt}</span> dari <span className="mono">{job.max_attempts}</span>
            </span>
          </div>
        </div>
        <div className="d-flex flex-wrap gap-2">
          <Link className="btn btn-sm btn-outline-secondary" to={requestPath(db, job.change_request_id)}>
            <i className="fas fa-clipboard-check" aria-hidden="true" />
            Lihat permintaan
          </Link>
          {job.status === 'succeeded' && (
            <Link className="btn btn-sm btn-outline-secondary" to={historyPath(db)}>
              <i className="fas fa-clock-rotate-left" aria-hidden="true" />
              Lihat riwayat
            </Link>
          )}
          <button type="button" className="btn btn-sm btn-outline-secondary" disabled={query.isFetching} onClick={() => void query.refetch()}>
            <i className={`fas fa-rotate-right${query.isFetching ? ' fa-spin' : ''}`} aria-hidden="true" />
            Muat ulang
          </button>
        </div>
      </div>

      {flash && (
        <Notice kind="success" onDismiss={() => setFlash(null)}>
          {flash}
        </Notice>
      )}
      <JobStepper status={job.status} />
      <p className="small text-muted" role="status">
        {outcomes[job.status]} {polling ? 'Diperbarui otomatis.' : 'Pembaruan otomatis berhenti.'}
      </p>
      {blocking && !recoverable && <Notice kind="warning">Job ini sedang menahan database sehingga perubahan baru belum bisa diajukan sampai hasilnya jelas.</Notice>}
      {job.error && (
        <Notice kind="error">
          <JobFailureText error={job.error} />
        </Notice>
      )}

      <div className="row g-4">
        <div className="col-lg-6">
          <div className="card mb-0 h-100">
            <div className="card-header">
              <i className="fas fa-bars-progress text-muted" aria-hidden="true" />
              <h2 className="h6 mb-0">Progres</h2>
            </div>
            <div className="card-body">
              <JobProgress job={job} />
            </div>
          </div>
        </div>
        <div className="col-lg-6">
          <div className="card mb-0 h-100">
            <div className="card-header">
              <i className="fas fa-stopwatch text-muted" aria-hidden="true" />
              <h2 className="h6 mb-0">Waktu</h2>
            </div>
            <div className="card-body">
              <dl className="facts mb-0">
                <dt>Masuk antrean</dt>
                <dd>
                  <time dateTime={job.queued_at}>{formatDateTime(job.queued_at)}</time>
                </dd>
                <dt>Waktu tunggu antrean</dt>
                <dd className="mono">{waited === null ? '-' : formatDuration(waited)}</dd>
                <dt>Mulai</dt>
                <dd>{job.started_at ? <time dateTime={job.started_at}>{formatDateTime(job.started_at)}</time> : '-'}</dd>
                <dt>Durasi</dt>
                <dd className="mono">{ran === null ? '-' : formatDuration(ran)}</dd>
                <dt>Selesai</dt>
                <dd>{job.finished_at ? <time dateTime={job.finished_at}>{formatDateTime(job.finished_at)}</time> : '-'}</dd>
              </dl>
            </div>
          </div>
        </div>
      </div>

      <div className="card mt-4">
        <div className="card-header">
          <i className="fas fa-list-ol text-muted" aria-hidden="true" />
          <h2 className="h6 mb-0">Percobaan</h2>
          <span className="card-header-meta">{attempts.data?.length ?? 0} percobaan</span>
        </div>
        {attempts.isError ? <ApiErrorView error={attempts.error} onRetry={() => void attempts.refetch()} /> : <AttemptsTable attempts={attempts.data ?? []} now={now} />}
      </div>

      {needsRecovery && canEdit && <RecoveryActions db={db} job={job} migration={migration.data} />}
      {needsRecovery && !canEdit && <Notice kind="warning">Job ini perlu dipulihkan oleh pengguna dengan akses ubah pada database ini.</Notice>}
      {recovered && (
        <Notice kind="info">
          {blocker ? (
            <>
              Job ini sudah digantikan oleh <Link to={jobPath(db, blocker)}>job pemulihan yang kini menahan database</Link>.
            </>
          ) : (
            'Job ini sudah dipulihkan sehingga database tidak lagi ditahan olehnya.'
          )}
        </Notice>
      )}
      {migration.data && (
        <p className="small">
          <Link to={migrationPath(db, migration.data.id)}>Lihat definisi migration</Link>
        </p>
      )}
    </>
  )
}
