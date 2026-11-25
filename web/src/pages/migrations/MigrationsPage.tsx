import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { migrationsQuery } from '../../api/migrations.ts'
import { ApiErrorView } from '../../components/ui/ApiErrorView.tsx'
import { Pager } from '../../components/ui/Pager.tsx'
import { BoxSkeleton } from '../../components/ui/Skeleton.tsx'
import { formatDateTime } from '../../lib/format.ts'
import { definitionStateLabel, modeLabel } from '../../lib/labels.ts'
import { pageSearch, readPage } from '../../lib/pagination.ts'
import { migrationPath, newMigrationPath } from '../../lib/paths.ts'
import { useDatabase } from '../../lib/useDatabase.ts'

export function MigrationsPage() {
  const { db, canEdit } = useDatabase()
  const [params] = useSearchParams()
  const page = readPage(params)
  const query = useQuery(migrationsQuery(db, page))
  const data = query.data

  return (
    <>
      <div className="d-flex flex-wrap align-items-center justify-content-between gap-2 mb-3">
        <h2 className="h5 mb-0">Daftar migration</h2>
        {canEdit && (
          <Link className="btn btn-primary" to={newMigrationPath(db)}>
            <i className="fas fa-plus" aria-hidden="true" />
            Buat migration
          </Link>
        )}
      </div>
      {query.isPending && <BoxSkeleton rows={5} />}
      {query.isError && <ApiErrorView error={query.error} onRetry={() => void query.refetch()} />}
      {data && (
        <>
          <div className="box box-primary mb-0" aria-busy={query.isPlaceholderData}>
            <div className="box-body">
              <div className={`table-responsive${query.isPlaceholderData ? ' opacity-50' : ''}`}>
                <table className="table table-hover" aria-label="Daftar migration">
                  <thead>
                    <tr>
                      <th scope="col">Judul</th>
                      <th scope="col">Mode</th>
                      <th scope="col">Status</th>
                      <th scope="col">Revision</th>
                      <th scope="col">Diperbarui</th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.items.map((migration) => (
                      <tr key={migration.id}>
                        <td>
                          <Link to={migrationPath(db, migration.id)}>{migration.title}</Link>{' '}
                          {migration.irreversible && <span className="badge badge-soft">Irreversible</span>}
                        </td>
                        <td>{modeLabel(migration.mode)}</td>
                        <td className="text-nowrap">
                          <span className={`badge status${migration.state === 'frozen' ? ' status-pending' : ''}`}>{definitionStateLabel(migration.state)}</span>
                          {migration.first_execution_at && <span className="badge status status-applied ms-1">Pernah dijalankan</span>}
                        </td>
                        <td className="mono">{migration.revision}</td>
                        <td className="text-nowrap">
                          <time dateTime={migration.updated_at}>{formatDateTime(migration.updated_at)}</time>
                        </td>
                      </tr>
                    ))}
                    {data.items.length === 0 && (
                      <tr>
                        <td colSpan={5} className="text-muted">
                          {data.page > 1 ? (
                            <>
                              Tidak ada migration di halaman ini. <Link to={{ search: '' }}>Kembali ke halaman pertama</Link>
                            </>
                          ) : (
                            'Belum ada migration untuk database ini.'
                          )}
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          </div>
          {(data.page > 1 || data.has_next) && <Pager page={data.page} hasNext={data.has_next} searchFor={pageSearch} info={`Halaman ${data.page}`} label="Halaman migration" />}
        </>
      )}
    </>
  )
}
