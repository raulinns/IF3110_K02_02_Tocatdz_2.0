import type { Schemas } from '../../api/types.ts'

interface JobProgressProps {
  job: Schemas['Job']
}

// Progres batch dihitung dari batch yang terbukti di-commit, dan total yang belum diketahui ditampilkan sebagai bar tanpa persentase
export function JobProgress({ job }: JobProgressProps) {
  const { progress } = job
  const active = job.status === 'running' || job.status === 'queued'
  if (job.mode === 'normal') {
    return (
      <p className="mb-0 text-muted">
        {job.status === 'running' ? 'Transaksi sedang dijalankan pada database target.' : job.status === 'queued' ? 'Menunggu worker mengambil job ini.' : 'Mode normal dijalankan dalam satu transaksi sehingga tidak punya progres per batch.'}
      </p>
    )
  }
  const total = progress.total_batches
  const percent = total && total > 0 ? Math.min(100, Math.round((progress.batches_committed / total) * 100)) : null
  return (
    <>
      <div
        className="progress"
        role="progressbar"
        aria-label="Progres batch"
        aria-valuemin={0}
        aria-valuemax={total ?? undefined}
        aria-valuenow={percent === null ? undefined : progress.batches_committed}
        aria-valuetext={percent === null ? `${progress.batches_committed} batch di-commit, total belum diketahui` : `${progress.batches_committed} dari ${total} batch`}
        style={{ height: '1.25rem' }}
      >
        <div className={`progress-bar${percent === null && active ? ' progress-bar-striped progress-bar-animated' : ''}`} style={{ width: percent === null ? (active ? '100%' : '0%') : `${percent}%` }}>
          {percent === null ? '' : `${percent}%`}
        </div>
      </div>
      <dl className="facts mt-3 mb-0">
        <dt>Batch di-commit</dt>
        <dd className="mono">{total === null ? progress.batches_committed : `${progress.batches_committed} dari ${total}`}</dd>
        <dt>Baris di-commit</dt>
        <dd className="mono">{progress.rows_committed}</dd>
        {(progress.batches_reverted > 0 || progress.rows_reverted > 0) && (
          <>
            <dt>Batch dibatalkan</dt>
            <dd className="mono">{progress.batches_reverted}</dd>
            <dt>Baris dibatalkan</dt>
            <dd className="mono">{progress.rows_reverted}</dd>
          </>
        )}
      </dl>
    </>
  )
}
