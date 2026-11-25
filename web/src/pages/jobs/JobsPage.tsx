import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { allMigrationsQuery, isFinalJob, jobsQuery, positionQuery } from '../../api/workflow.ts'
import type { Schemas } from '../../api/types.ts'
import { ApiErrorView } from '../../components/ui/ApiErrorView.tsx'
import { Notice } from '../../components/ui/Notice.tsx'
import { Pager } from '../../components/ui/Pager.tsx'
import { BoxSkeleton } from '../../components/ui/Skeleton.tsx'
import { StatusBadge } from '../../components/ui/StatusBadge.tsx'
import { elapsed, formatDuration } from '../../lib/duration.ts'
import { formatDateTime } from '../../lib/format.ts'
import { changeKindLabel, runStatusLabel } from '../../lib/labels.ts'
import { readPage } from '../../lib/pagination.ts'
import { jobPath } from '../../lib/paths.ts'
import { useDatabase } from '../../lib/useDatabase.ts'
import { useNow } from '../../lib/useNow.ts'

const statuses: Schemas['JobStatus'][] = ['queued', 'running', 'succeeded', 'failed', 'partial', 'uncertain', 'partially_reverted']
const readStatus = (value: string | null) => statuses.find((status) => status === value)

export function JobsPage() {
  const { db } = useDatabase()
  const [params, setParams] = useSearchParams()
  const page = readPage(params)
  const status = readStatus(params.get('status'))
  const query = useQuery(jobsQuery(db, page, status))
  const migrations = useQuery(allMigrationsQuery(db))
  const position = useQuery(positionQuery(db))
  const data = query.data
  // Durasi job yang belum selesai bertambah tiap detik sehingga jam hanya berjalan bila ada job aktif di halaman ini
  const now = useNow(data?.items.some((job) => !isFinalJob(job.status)) ?? false)
  const titleOf = (id: string) => migrations.data?.find((migration) => migration.id === id)?.title
  const blocker = position.data?.blocked_by_job_id

  const search = (next: { status?: string; page?: number }) => {
    const target = new URLSearchParams()
    if (next.status) target.set('status', next.status)
    if (next.page && next.page > 1) target.set('page', String(next.page))
    const text = target.toString()
    return text ? `?${text}` : ''
  }

  return (
    <>
      <div className="d-flex flex-wrap align-items-center justify-content-between gap-2 mb-3">
        <h2 className="h5 mb-0">Monitor job</h2>
        <div className="d-flex align-items-center gap-2">
          <label htmlFor="job-status" className="form-label mb-0 small text-muted">
            Status
          </label>
          <select id="job-status" className="form-select form-select-sm w-auto" value={status ?? ''} onChange={(event) => setParams(search({ status: event.target.value }))}>
            <option value="">Semua</option>
            {statuses.map((option) => (
              <option key={option} value={option}>
                {runStatusLabel(option)}
              </option>
            ))}
          </select>
        </div>
      </div>
      {blocker && (
        <Notice kind="warning">
          Database sedang ditahan oleh <Link to={jobPath(db, blocker)}>sebuah job</Link> yang belum selesai atau belum dipulihkan. Perubahan baru belum bisa diajukan sampai itu selesai.
        </Notice>
      )}
      {query.isPending && <BoxSkeleton rows={5} />}
      {query.isError && <ApiErrorView error={query.error} onRetry={() => void query.refetch()} />}
      {data && (
        <>
          <div className="box box-primary mb-0" aria-busy={query.isPlaceholderData}>
            <div className="box-body">
              <div className={`table-responsive${query.isPlaceholderData ? ' opacity-50' : ''}`}>
                <table className="table table-hover" aria-label="Daftar job">
                  <thead>
                    <tr>
                      <th scope="col">Migration</th>
                      <th scope="col">Jenis</th>
                      <th scope="col">Status</th>
                      <th scope="col">Percobaan</th>
                      <th scope="col">Masuk antrean</th>
                      <th scope="col">Durasi</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.items.map((job) => {
                      const duration = elapsed(job.started_at, job.finished_at, now)
                      return (
                        <tr key={job.id}>
                          <td>
                            <Link to={jobPath(db, job.id)}>{titleOf(job.migration_id) ?? `Migration ${job.migration_id.slice(0, 8)}`}</Link>
                          </td>
                          <td>{changeKindLabel(job.kind)}</td>
                          <td>
                            <StatusBadge status={job.status} />
                          </td>
                          <td className="mono">
                            {job.attempt}/{job.max_attempts}
                          </td>
                          <td className="text-nowrap">
                            <time dateTime={job.queued_at}>{formatDateTime(job.queued_at)}</time>
                          </td>
                          <td className="text-nowrap">{duration === null ? '-' : formatDuration(duration)}</td>
                        </tr>
                      )
                    })}
                    {data.items.length === 0 && (
                      <tr>
                        <td colSpan={6} className="text-muted">
                          {data.page > 1 ? (
                            <>
                              Tidak ada job di halaman ini. <Link to={{ search: search({ status }) }}>Kembali ke halaman pertama</Link>
                            </>
                          ) : status ? (
                            `Tidak ada job dengan status ${runStatusLabel(status).toLowerCase()}.`
                          ) : (
                            'Belum ada job untuk database ini.'
                          )}
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          </div>
          {(data.page > 1 || data.has_next) && <Pager page={data.page} hasNext={data.has_next} searchFor={(target) => search({ status, page: target })} info={`Halaman ${data.page}`} label="Halaman job" />}
        </>
      )}
    </>
  )
}
