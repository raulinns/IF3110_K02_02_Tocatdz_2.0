import type { Schemas } from '../../api/types.ts'
import { StatusBadge } from '../../components/ui/StatusBadge.tsx'
import { elapsed, formatDuration } from '../../lib/duration.ts'
import { formatDateTime } from '../../lib/format.ts'

interface AttemptsTableProps {
  attempts: Schemas['JobAttempt'][]
  now: number
}

export function JobFailureText({ error }: { error: Schemas['JobFailure'] }) {
  return (
    <>
      <div>{error.message}</div>
      <div className="small text-muted mono">
        {error.code}
        {error.sqlstate ? ` · SQLSTATE ${error.sqlstate}` : ''}
        {error.retryable ? ' · dapat diulang' : ''}
      </div>
    </>
  )
}

// Semua percobaan ditampilkan termasuk yang gagal, karena job bisa dicoba ulang otomatis
export function AttemptsTable({ attempts, now }: AttemptsTableProps) {
  return (
    <div className="table-responsive">
      <table className="table table-hover mb-0" aria-label="Percobaan job">
        <thead>
          <tr>
            <th scope="col">Percobaan</th>
            <th scope="col">Status</th>
            <th scope="col">Mulai</th>
            <th scope="col">Durasi</th>
            <th scope="col">Hasil</th>
          </tr>
        </thead>
        <tbody>
          {attempts.map((attempt) => {
            const duration = elapsed(attempt.started_at, attempt.finished_at, now)
            return (
              <tr key={attempt.attempt}>
                <td className="mono">{attempt.attempt}</td>
                <td>
                  <StatusBadge status={attempt.status} />
                </td>
                <td className="text-nowrap">
                  <time dateTime={attempt.started_at}>{formatDateTime(attempt.started_at)}</time>
                </td>
                <td className="text-nowrap">{duration === null ? '-' : formatDuration(duration)}</td>
                <td className="diff-cell">{attempt.error ? <JobFailureText error={attempt.error} /> : '-'}</td>
              </tr>
            )
          })}
          {attempts.length === 0 && (
            <tr>
              <td colSpan={5} className="text-muted">
                Belum ada percobaan. Job menunggu diambil worker.
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  )
}
