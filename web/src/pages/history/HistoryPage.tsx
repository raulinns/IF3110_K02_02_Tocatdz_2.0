import { useEffect, useRef, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Link, useSearchParams } from 'react-router-dom'
import { allMigrationsQuery, executionsQuery, positionQuery } from '../../api/workflow.ts'
import { ApiErrorView } from '../../components/ui/ApiErrorView.tsx'
import { Pager } from '../../components/ui/Pager.tsx'
import { BoxSkeleton } from '../../components/ui/Skeleton.tsx'
import { readPage } from '../../lib/pagination.ts'
import { useDatabase } from '../../lib/useDatabase.ts'
import { ExecutionRow } from './ExecutionRow.tsx'
import { PositionCard } from './PositionCard.tsx'

const highlightMs = 2000

export function HistoryPage() {
  const { db, canEdit } = useDatabase()
  const [params, setParams] = useSearchParams()
  const page = readPage(params)
  // Hide rollback tersimpan di URL supaya pilihan ini ikut saat tautan dibagikan atau halaman dimuat ulang
  const hide = params.get('hide_rollback') === 'true'
  const query = useQuery(executionsQuery(db, page, hide))
  const position = useQuery(positionQuery(db))
  const migrations = useQuery(allMigrationsQuery(db))
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(new Set())
  const [highlighted, setHighlighted] = useState<string | null>(null)
  const timer = useRef<number>(undefined)
  const data = query.data
  const titleOf = (id: string | null) => (id ? migrations.data?.find((migration) => migration.id === id)?.title : undefined)

  useEffect(() => () => window.clearTimeout(timer.current), [])

  const search = (next: { hide: boolean; page?: number }) => {
    const target = new URLSearchParams()
    if (next.hide) target.set('hide_rollback', 'true')
    if (next.page && next.page > 1) target.set('page', String(next.page))
    const text = target.toString()
    return text ? `?${text}` : ''
  }

  const toggle = (id: string) =>
    setExpanded((current) => {
      const next = new Set(current)
      if (!next.delete(id)) next.add(id)
      return next
    })

  // Menuju baris eksekusi terkait lalu menandainya sebentar supaya mudah ditemukan
  const jump = (id: string) => {
    const row = document.getElementById(`execution-${id}`)
    row?.scrollIntoView?.({ block: 'center' })
    row?.focus()
    setHighlighted(id)
    window.clearTimeout(timer.current)
    timer.current = window.setTimeout(() => setHighlighted(null), highlightMs)
  }

  const numberOf = (id: string) => data?.items.find((execution) => execution.id === id)?.position_version

  return (
    <>
      {position.isError && <ApiErrorView error={position.error} onRetry={() => void position.refetch()} />}
      {position.data && <PositionCard db={db} position={position.data} title={titleOf} canEdit={canEdit} />}

      <div className="d-flex flex-wrap align-items-center justify-content-between gap-2 mb-3">
        <h2 className="h5 mb-0">Riwayat eksekusi</h2>
        <div className="form-check mb-0">
          <input className="form-check-input" type="checkbox" id="hide-rollback" checked={hide} aria-describedby="hide-rollback-hint" onChange={(event) => setParams(search({ hide: event.target.checked }))} />
          <label className="form-check-label" htmlFor="hide-rollback">
            Sembunyikan rollback
          </label>
          <div id="hide-rollback-hint" className="form-text mt-0">
            Menyembunyikan eksekusi yang sudah di-rollback beserta rollback-nya.
          </div>
        </div>
      </div>

      {query.isPending && <BoxSkeleton rows={6} />}
      {query.isError && <ApiErrorView error={query.error} onRetry={() => void query.refetch()} />}
      {data && (
        <>
          <div className="box box-primary mb-0" aria-busy={query.isPlaceholderData}>
            <div className="box-body">
              <div className={`table-responsive${query.isPlaceholderData ? ' opacity-50' : ''}`}>
                <table className="table table-hover" aria-label="Riwayat eksekusi">
                  <thead>
                    <tr>
                      <th scope="col">Posisi</th>
                      <th scope="col">Waktu</th>
                      <th scope="col">Migration</th>
                      <th scope="col">Operasi</th>
                      <th scope="col">Aktor</th>
                      <th scope="col">Terkait</th>
                      <th scope="col" className="text-end">
                        <span className="visually-hidden">Detail</span>
                      </th>
                    </tr>
                  </thead>
                  <tbody>
                    {data.items.map((execution) => (
                      <ExecutionRow
                        key={execution.id}
                        db={db}
                        execution={execution}
                        title={titleOf(execution.migration_id)}
                        active={execution.id === position.data?.active_execution_id}
                        highlighted={execution.id === highlighted}
                        expanded={expanded.has(execution.id)}
                        onToggle={() => toggle(execution.id)}
                        numberOf={numberOf}
                        onJump={jump}
                      />
                    ))}
                    {data.items.length === 0 && (
                      <tr>
                        <td colSpan={7} className="text-muted">
                          {data.page > 1 ? (
                            <>
                              Tidak ada eksekusi di halaman ini. <Link to={{ search: search({ hide }) }}>Kembali ke halaman pertama</Link>
                            </>
                          ) : (
                            'Belum ada eksekusi untuk database ini.'
                          )}
                        </td>
                      </tr>
                    )}
                  </tbody>
                </table>
              </div>
            </div>
          </div>
          {(data.page > 1 || data.has_next) && <Pager page={data.page} hasNext={data.has_next} searchFor={(target) => search({ hide, page: target })} info={`Halaman ${data.page}`} label="Halaman riwayat" />}
        </>
      )}
    </>
  )
}
