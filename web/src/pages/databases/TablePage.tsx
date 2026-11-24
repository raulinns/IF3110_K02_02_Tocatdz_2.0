import type { ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useParams } from 'react-router-dom'
import { tableQuery } from '../../api/explorer.ts'
import { ApiErrorView } from '../../components/ui/ApiErrorView.tsx'
import { Breadcrumbs } from '../../components/ui/Breadcrumbs.tsx'
import { BoxSkeleton } from '../../components/ui/Skeleton.tsx'
import { tableKindLabel } from '../../lib/labels.ts'
import { rowsPath, schemaPath } from '../../lib/paths.ts'

interface DetailCardProps {
  icon: string
  title: string
  count: number
  heads: string[]
  empty: string
  children: ReactNode
}

function DetailCard({ icon, title, count, heads, empty, children }: DetailCardProps) {
  return (
    <div className="card">
      <div className="card-header">
        <i className={`fas ${icon} text-muted`} aria-hidden="true" />
        <h2 className="h6 mb-0">{title}</h2>
        <span className="card-header-meta">{count}</span>
      </div>
      <div className="table-responsive">
        <table className="table table-hover" aria-label={title}>
          <thead>
            <tr>
              {heads.map((head) => (
                <th scope="col" key={head}>
                  {head}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {count > 0 ? (
              children
            ) : (
              <tr>
                <td colSpan={heads.length} className="text-muted">
                  {empty}
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

export function TablePage() {
  const { db = '', schema = '', table = '' } = useParams()
  const query = useQuery(tableQuery(db, schema, table))
  const detail = query.data

  return (
    <>
      <Breadcrumbs items={[{ label: 'Schema', to: schemaPath(db) }, { label: `${schema}.${table}` }]} />
      <div className="d-flex flex-wrap align-items-center justify-content-between gap-2 mb-3">
        <h2 className="h5 mono mb-0">
          {schema}.{table} {detail && <span className="badge badge-soft align-middle fs-6">{tableKindLabel(detail.kind)}</span>}
        </h2>
        {detail && (
          <Link className="btn btn-primary" to={rowsPath(db, schema, table)}>
            <i className="fas fa-eye" aria-hidden="true" />
            Lihat data
          </Link>
        )}
      </div>
      {query.isPending && <BoxSkeleton rows={6} />}
      {query.isError && <ApiErrorView error={query.error} onRetry={() => void query.refetch()} />}
      {detail && (
        <>
          <DetailCard icon="fa-table-columns" title="Kolom" count={detail.columns.length} heads={['Nama', 'Tipe', 'Nullable', 'Default']} empty="Tidak ada kolom.">
            {detail.columns.map((column) => (
              <tr key={column.name}>
                <td className="mono">{column.name}</td>
                <td className="mono">{column.data_type}</td>
                <td>{column.nullable ? 'Ya' : 'Tidak'}</td>
                <td className="mono">{column.default ?? '-'}</td>
              </tr>
            ))}
          </DetailCard>
          <DetailCard icon="fa-link" title="Constraint" count={detail.constraints.length} heads={['Nama', 'Jenis', 'Definisi']} empty="Tidak ada constraint.">
            {detail.constraints.map((constraint) => (
              <tr key={constraint.name}>
                <td className="mono">{constraint.name}</td>
                <td>
                  <span className="badge badge-soft">{constraint.type}</span>
                </td>
                <td className="mono">{constraint.definition}</td>
              </tr>
            ))}
          </DetailCard>
          <DetailCard icon="fa-bolt" title="Index" count={detail.indexes.length} heads={['Nama', 'Unik', 'Definisi']} empty="Tidak ada index.">
            {detail.indexes.map((index) => (
              <tr key={index.name}>
                <td className="mono">{index.name}</td>
                <td>
                  {index.unique ? 'Ya' : 'Tidak'}
                  {index.primary && ' (primary key)'}
                </td>
                <td className="mono">{index.definition}</td>
              </tr>
            ))}
          </DetailCard>
        </>
      )}
    </>
  )
}
