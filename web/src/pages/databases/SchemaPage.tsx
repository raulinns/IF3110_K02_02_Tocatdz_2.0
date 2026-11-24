import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import { schemaQuery } from '../../api/explorer.ts'
import { ApiErrorView } from '../../components/ui/ApiErrorView.tsx'
import { EmptyState } from '../../components/ui/EmptyState.tsx'
import { BoxSkeleton } from '../../components/ui/Skeleton.tsx'
import { tableKindLabel } from '../../lib/labels.ts'
import { tablePath } from '../../lib/paths.ts'

export function SchemaPage() {
  const { db = '' } = useParams()
  const query = useQuery(schemaQuery(db))
  const [filter, setFilter] = useState('')

  if (query.isPending) return <BoxSkeleton />
  if (query.isError) return <ApiErrorView error={query.error} onRetry={() => void query.refetch()} />

  const { schemas } = query.data
  const total = schemas.reduce((sum, schema) => sum + schema.tables.length, 0)
  const term = filter.trim().toLowerCase()
  const visible = schemas
    .map((schema) => ({ ...schema, tables: term ? schema.tables.filter((table) => table.name.toLowerCase().includes(term)) : schema.tables }))
    .filter((schema) => !term || schema.tables.length > 0)

  if (schemas.length === 0) return <EmptyState icon="fa-circle-info">Database ini belum memiliki schema yang dapat ditampilkan.</EmptyState>

  return (
    <>
      {total > 0 && (
        <div className="mb-3" style={{ maxWidth: '22rem' }}>
          <label htmlFor="table-filter" className="visually-hidden">
            Cari table
          </label>
          <input id="table-filter" type="search" className="form-control" placeholder="Cari table" autoComplete="off" value={filter} onChange={(event) => setFilter(event.target.value)} />
        </div>
      )}
      {visible.length === 0 && <EmptyState icon="fa-magnifying-glass">Tidak ada table yang cocok dengan &ldquo;{filter.trim()}&rdquo;.</EmptyState>}
      {visible.map((schema) => (
        <div className="card" key={schema.name}>
          <div className="card-header">
            <i className="fas fa-folder-open text-muted" aria-hidden="true" />
            <h2 className="h6 mb-0 mono">{schema.name}</h2>
            <span className="card-header-meta">{schema.tables.length} table</span>
          </div>
          {schema.tables.length > 0 ? (
            <div className="table-responsive">
              <table className="table table-hover" aria-label={`Table pada schema ${schema.name}`}>
                <thead>
                  <tr>
                    <th scope="col">Nama</th>
                    <th scope="col">Jenis</th>
                  </tr>
                </thead>
                <tbody>
                  {schema.tables.map((table) => (
                    <tr key={table.name}>
                      <td className="mono">
                        <Link to={tablePath(db, schema.name, table.name)}>
                          <i className="fas fa-table text-muted" aria-hidden="true" /> {table.name}
                        </Link>
                      </td>
                      <td>
                        <span className="badge badge-soft">{tableKindLabel(table.kind)}</span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <div className="empty-state">
              <i className="fas fa-circle-info" aria-hidden="true" />
              <p className="mb-0">Schema ini belum memiliki table.</p>
            </div>
          )}
        </div>
      ))}
    </>
  )
}
