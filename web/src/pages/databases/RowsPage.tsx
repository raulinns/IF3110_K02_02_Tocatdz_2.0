import { useQuery } from '@tanstack/react-query'
import { Link, useParams, useSearchParams } from 'react-router-dom'
import { defaultRowLimit, rowLimits, rowsQuery } from '../../api/explorer.ts'
import type { Schemas } from '../../api/types.ts'
import { ApiErrorView } from '../../components/ui/ApiErrorView.tsx'
import { Breadcrumbs } from '../../components/ui/Breadcrumbs.tsx'
import { Pager } from '../../components/ui/Pager.tsx'
import { BoxSkeleton } from '../../components/ui/Skeleton.tsx'
import { positiveInteger, readPage } from '../../lib/pagination.ts'
import { schemaPath, tablePath } from '../../lib/paths.ts'

const isRowLimit = (value: number | undefined): value is (typeof rowLimits)[number] => rowLimits.some((limit) => limit === value)

// Halaman pertama dan jumlah baris bawaan tidak ditulis ke URL supaya tautannya tetap pendek
function search(page: number, limit: number): string {
  const params = new URLSearchParams()
  if (page > 1) params.set('page', String(page))
  if (limit !== defaultRowLimit) params.set('limit', String(limit))
  const text = params.toString()
  return text ? `?${text}` : ''
}

// Penanda ditaruh sebelum teks supaya tetap terlihat walau teks panjang dipotong elipsis oleh sel
function CellValue({ cell }: { cell: Schemas['Cell'] }) {
  if (cell.null) return <span className="null">NULL</span>
  return (
    <>
      {cell.encoding === 'hex' && <span className="badge badge-soft me-1">hex</span>}
      {cell.truncated && <span className="badge badge-soft me-1">dipotong</span>}
      {cell.text}
    </>
  )
}

export function RowsPage() {
  const { db = '', schema = '', table = '' } = useParams()
  const [params, setParams] = useSearchParams()
  const page = readPage(params)
  const requestedLimit = positiveInteger(params.get('limit'))
  const limit = isRowLimit(requestedLimit) ? requestedLimit : defaultRowLimit
  const query = useQuery(rowsQuery(db, schema, table, page, limit))
  const data = query.data
  const name = `${schema}.${table}`

  return (
    <>
      <Breadcrumbs items={[{ label: 'Schema', to: schemaPath(db) }, { label: name, to: tablePath(db, schema, table) }, { label: 'Data' }]} />
      <div className="d-flex flex-wrap align-items-center justify-content-between gap-2 mb-3">
        <h2 className="h5 mb-0">
          Data <span className="mono">{name}</span>
        </h2>
        <div className="d-flex align-items-center gap-2">
          <label htmlFor="row-limit" className="form-label mb-0 small text-muted">
            Baris per halaman
          </label>
          <select id="row-limit" className="form-select form-select-sm w-auto" value={limit} onChange={(event) => setParams(search(1, Number(event.target.value)))}>
            {rowLimits.map((option) => (
              <option key={option} value={option}>
                {option}
              </option>
            ))}
          </select>
          <button type="button" className="btn btn-sm btn-outline-secondary" disabled={query.isFetching} onClick={() => void query.refetch()}>
            <i className={`fas fa-rotate-right${query.isFetching ? ' fa-spin' : ''}`} aria-hidden="true" />
            Muat ulang
          </button>
        </div>
      </div>
      {query.isPending && <BoxSkeleton rows={8} />}
      {query.isError && <ApiErrorView error={query.error} onRetry={() => void query.refetch()} />}
      {data && (
        <>
          {data.order === 'best_effort' && (
            <div className="alert alert-warning" role="note">
              <i className="fas fa-triangle-exclamation" aria-hidden="true" />
              <div>Tabel ini tidak punya urutan unik, isi halaman bisa bergeser saat data berubah.</div>
            </div>
          )}
          <div className="card" aria-busy={query.isPlaceholderData}>
            {data.items.length > 0 ? (
              <div className={`table-responsive${query.isPlaceholderData ? ' opacity-50' : ''}`} tabIndex={0} role="region" aria-label={`Isi tabel ${name}`}>
                <table className="table table-sm table-hover table-data" aria-label={`Isi tabel ${name} halaman ${data.page}`}>
                  <thead>
                    <tr>
                      {data.columns.map((column, index) => (
                        <th scope="col" className="mono" key={`${index}-${column}`}>
                          {column}
                        </th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {data.items.map((row, rowIndex) => (
                      <tr key={rowIndex}>
                        {row.map((cell, cellIndex) => (
                          <td className="mono" key={cellIndex} title={cell.null ? undefined : cell.text}>
                            <CellValue cell={cell} />
                          </td>
                        ))}
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <div className="empty-state">
                <i className="fas fa-circle-info" aria-hidden="true" />
                <p className="mb-0">{data.page > 1 ? 'Tidak ada baris di halaman ini.' : 'Tabel ini belum memiliki baris.'}</p>
                {data.page > 1 && (
                  <Link className="btn btn-sm btn-light mt-3" to={{ search: search(1, limit) }}>
                    Kembali ke halaman pertama
                  </Link>
                )}
              </div>
            )}
          </div>
          <Pager page={data.page} hasNext={data.has_next} searchFor={(target) => search(target, limit)} info={`Halaman ${data.page} · ${data.limit} baris per halaman`} />
        </>
      )}
    </>
  )
}
