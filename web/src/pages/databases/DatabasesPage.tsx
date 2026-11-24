import { useQuery } from '@tanstack/react-query'
import { Link } from 'react-router-dom'
import { databasesQuery } from '../../api/explorer.ts'
import { AccessBadge } from '../../components/ui/AccessBadge.tsx'
import { ApiErrorView } from '../../components/ui/ApiErrorView.tsx'
import { EmptyState } from '../../components/ui/EmptyState.tsx'
import { PageHeader } from '../../components/ui/PageHeader.tsx'
import { BoxSkeleton } from '../../components/ui/Skeleton.tsx'
import { engineLabel } from '../../lib/labels.ts'
import { databasePath, schemaPath } from '../../lib/paths.ts'

export function DatabasesPage() {
  const query = useQuery(databasesQuery())

  return (
    <>
      <PageHeader pretitle="Platform" title="Database" />
      {query.isPending && <BoxSkeleton />}
      {query.isError && <ApiErrorView error={query.error} onRetry={() => void query.refetch()} />}
      {query.isSuccess && query.data.length === 0 && <EmptyState icon="fa-lock">Anda belum memiliki akses ke database mana pun. Hubungi admin platform untuk meminta akses.</EmptyState>}
      {query.isSuccess && query.data.length > 0 && (
        <div className="box box-primary">
          <div className="box-body">
            <div className="table-responsive">
              <table className="table table-hover" aria-label="Database yang dapat Anda akses">
                <thead>
                  <tr>
                    <th scope="col">Nama</th>
                    <th scope="col">Akses</th>
                    <th scope="col" className="text-end">
                      Tindakan
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {query.data.map((database) => (
                    <tr key={database.id}>
                      <td className="mono text-nowrap">
                        <i className="fas fa-database text-muted" aria-hidden="true" /> {database.name} <span className="badge badge-soft">{engineLabel(database.engine)}</span>
                      </td>
                      <td>
                        <AccessBadge access={database.access} />
                      </td>
                      <td className="text-end text-nowrap">
                        <Link className="btn btn-sm btn-outline-primary" to={schemaPath(database.id)}>
                          <i className="fas fa-table" aria-hidden="true" />
                          Schema
                        </Link>
                        {database.capabilities.workflow && (
                          <Link className="btn btn-sm btn-outline-secondary ms-1" to={`${databasePath(database.id)}/migrations`}>
                            <i className="fas fa-code-branch" aria-hidden="true" />
                            Migration
                          </Link>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          </div>
        </div>
      )}
    </>
  )
}
