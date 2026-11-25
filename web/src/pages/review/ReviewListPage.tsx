import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { allMigrationsQuery, requestsQuery } from '../../api/workflow.ts'
import type { Schemas } from '../../api/types.ts'
import { useSession } from '../../auth/session-context.ts'
import { ApiErrorView } from '../../components/ui/ApiErrorView.tsx'
import { Pager } from '../../components/ui/Pager.tsx'
import { BoxSkeleton } from '../../components/ui/Skeleton.tsx'
import { StatusBadge } from '../../components/ui/StatusBadge.tsx'
import { formatDateTime } from '../../lib/format.ts'
import { changeKindLabel, runStatusLabel } from '../../lib/labels.ts'
import { readPage } from '../../lib/pagination.ts'
import { requestPath } from '../../lib/paths.ts'
import { awaitsDecisionFrom } from '../../lib/review.ts'
import { useDatabase } from '../../lib/useDatabase.ts'
import { useUsernames } from '../../lib/useUsernames.ts'

const statuses: Schemas['CRStatus'][] = ['pending_review', 'approved', 'rejected', 'queued', 'running', 'succeeded', 'failed', 'partial', 'uncertain', 'partially_reverted']

const readStatus = (value: string | null) => statuses.find((status) => status === value)

export function ReviewListPage() {
  const { db } = useDatabase()
  const session = useSession()
  const me = session.status === 'authenticated' ? session.session.user.id : ''
  const [params, setParams] = useSearchParams()
  const page = readPage(params)
  const status = readStatus(params.get('status'))
  const query = useQuery(requestsQuery(db, page, status))
  const migrations = useQuery(allMigrationsQuery(db))
  const names = useUsernames()
  const data = query.data
  const titleOf = (id: string) => migrations.data?.find((migration) => migration.id === id)?.title

  // Filter dan halaman tersimpan di URL, dan mengganti filter selalu kembali ke halaman pertama
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
        <h2 className="h5 mb-0">Permintaan perubahan</h2>
        <div className="d-flex align-items-center gap-2">
          <label htmlFor="status-filter" className="form-label mb-0 small text-muted">
            Status
          </label>
          <select id="status-filter" className="form-select form-select-sm w-auto" value={status ?? ''} onChange={(event) => setParams(search({ status: event.target.value }))}>
            <option value="">Semua</option>
            {statuses.map((option) => (
              <option key={option} value={option}>
                {runStatusLabel(option)}
              </option>
            ))}
          </select>
        </div>
      </div>
      {query.isPending && <BoxSkeleton rows={5} />}
      {query.isError && <ApiErrorView error={query.error} onRetry={() => void query.refetch()} />}
      {data && (
        <>
          <div className="box box-primary mb-0" aria-busy={query.isPlaceholderData}>
            <div className="box-body">
              <div className={`table-responsive${query.isPlaceholderData ? ' opacity-50' : ''}`}>
                <table className="table table-hover" aria-label="Daftar permintaan perubahan">
                  <thead>
                    <tr>
                      <th scope="col">Migration</th>
                      <th scope="col">Jenis</th>
                      <th scope="col">Status</th>
                      <th scope="col">Approval</th>
                      <th scope="col">Pengaju</th>
                      <th scope="col">Diajukan</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.items.map((request) => {
                      const approved = request.approvals.filter((approval) => approval.decision === 'approve').length
                      return (
                        <tr key={request.id}>
                          <td>
                            <Link to={requestPath(db, request.id)}>{titleOf(request.migration_id) ?? `Migration ${request.migration_id.slice(0, 8)}`}</Link>
                            {awaitsDecisionFrom(request, me) && (
                              <>
                                {' '}
                                <span className="badge status status-pending">Menunggu keputusan Anda</span>
                              </>
                            )}
                          </td>
                          <td>{changeKindLabel(request.kind)}</td>
                          <td>
                            <StatusBadge status={request.status} />
                          </td>
                          <td className="text-nowrap">
                            {approved}/{request.approvals.length} group
                          </td>
                          <td className="mono">{names(request.requested_by)}</td>
                          <td className="text-nowrap">
                            <time dateTime={request.created_at}>{formatDateTime(request.created_at)}</time>
                          </td>
                        </tr>
                      )
                    })}
                    {data.items.length === 0 && (
                      <tr>
                        <td colSpan={6} className="text-muted">
                          {data.page > 1 ? (
                            <>
                              Tidak ada permintaan di halaman ini. <Link to={{ search: search({ status }) }}>Kembali ke halaman pertama</Link>
                            </>
                          ) : status ? (
                            `Tidak ada permintaan dengan status ${runStatusLabel(status).toLowerCase()}.`
                          ) : (
                            'Belum ada permintaan perubahan untuk database ini.'
                          )}
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          </div>
          {(data.page > 1 || data.has_next) && <Pager page={data.page} hasNext={data.has_next} searchFor={(target) => search({ status, page: target })} info={`Halaman ${data.page}`} label="Halaman permintaan" />}
        </>
      )}
    </>
  )
}
