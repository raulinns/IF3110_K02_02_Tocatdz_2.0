import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { allUsersQuery, groupsQuery } from '../../api/admin.ts'
import { ApiErrorView } from '../../components/ui/ApiErrorView.tsx'
import { Notice } from '../../components/ui/Notice.tsx'
import { Pager } from '../../components/ui/Pager.tsx'
import { BoxSkeleton } from '../../components/ui/Skeleton.tsx'
import { pageSearch, readPage } from '../../lib/pagination.ts'
import { adminGroupPath } from '../../lib/paths.ts'
import { CreateGroupForm } from './CreateGroupForm.tsx'

export function GroupsPage() {
  const client = useQueryClient()
  const [params] = useSearchParams()
  const page = readPage(params)
  const query = useQuery(groupsQuery(page))
  const users = useQuery(allUsersQuery())
  const [notice, setNotice] = useState<string | null>(null)
  const data = query.data
  const usernameOf = (id: string) => users.data?.find((user) => user.id === id)?.username

  return (
    <div className="row g-4">
      <div className="col-xl-8">
        {notice && (
          <Notice kind="success" onDismiss={() => setNotice(null)}>
            {notice}
          </Notice>
        )}
        {query.isPending && <BoxSkeleton rows={5} />}
        {query.isError && <ApiErrorView error={query.error} onRetry={() => void query.refetch()} />}
        {data && (
          <>
            <div className="box box-primary mb-0" aria-busy={query.isPlaceholderData}>
              <div className="box-body">
                <div className={`table-responsive${query.isPlaceholderData ? ' opacity-50' : ''}`}>
                  <table className="table table-hover" aria-label="Daftar group">
                    <thead>
                      <tr>
                        <th scope="col">Group</th>
                        <th scope="col">Anggota</th>
                        <th scope="col">Leader</th>
                        <th scope="col" className="text-end">
                          Tindakan
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      {data.items.map((group) => (
                        <tr key={group.id}>
                          <td>
                            <i className="fas fa-users text-muted d-none d-md-inline" aria-hidden="true" /> {group.name}
                          </td>
                          <td>{group.member_count}</td>
                          <td>
                            {group.leader_user_id ? (
                              <span className="mono">{usernameOf(group.leader_user_id) ?? '-'}</span>
                            ) : (
                              <span className="badge status status-pending text-wrap">Belum ada leader</span>
                            )}
                          </td>
                          <td className="text-end">
                            <Link className="btn btn-sm btn-outline-primary" to={adminGroupPath(group.id)} aria-label={`Kelola group ${group.name}`} title="Kelola">
                              <i className="fas fa-gear" aria-hidden="true" />
                              <span className="d-none d-md-inline">Kelola</span>
                            </Link>
                          </td>
                        </tr>
                      ))}
                      {data.items.length === 0 && (
                        <tr>
                          <td colSpan={4} className="text-muted">
                            {data.page > 1 ? (
                              <>
                                Tidak ada group di halaman ini. <Link to={{ search: '' }}>Kembali ke halaman pertama</Link>
                              </>
                            ) : (
                              'Belum ada group.'
                            )}
                          </td>
                        </tr>
                      )}
                    </tbody>
                  </table>
                </div>
              </div>
            </div>
            {(data.page > 1 || data.has_next) && <Pager page={data.page} hasNext={data.has_next} searchFor={pageSearch} info={`Halaman ${data.page}`} label="Halaman group" />}
          </>
        )}
      </div>
      <div className="col-xl-4">
        <CreateGroupForm
          onCreated={(group) => {
            void client.invalidateQueries({ queryKey: ['admin', 'groups'] })
            setNotice(`Group ${group.name} berhasil dibuat.`)
          }}
        />
      </div>
    </div>
  )
}
