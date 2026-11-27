import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import type { Schemas } from '../../api/types.ts'
import { statementsPageSize, statementsQuery } from '../../api/workflow.ts'
import { ApiErrorView } from '../../components/ui/ApiErrorView.tsx'
import { BoxSkeleton } from '../../components/ui/Skeleton.tsx'

interface ExecutedStatementsProps {
  db: string
  executionId: string
  historyId?: string
}

// SQL yang benar-benar dikirim ke database target, per batch, dengan nilainya sudah disamarkan oleh server
export function ExecutedStatements({ db, executionId, historyId }: ExecutedStatementsProps) {
  const [page, setPage] = useState(1)
  const query = useQuery(statementsQuery(db, executionId, page, historyId))
  const heading = `statements-${historyId ?? executionId}`

  return (
    <section aria-labelledby={heading} className="mt-3">
      <h3 id={heading} className="h6">
        SQL yang dijalankan
      </h3>
      <p className="small text-muted">
        Dispatch dan hasil statement, bukan bukti commit transaksi. Nilai sensitif disamarkan sebagai [REDACTED]; bentuk yang tidak aman ditampilkan sebagai [REDACTED SQL]. Hasil yang belum tercatat tidak dianggap berhasil.
      </p>
      {query.isPending && <BoxSkeleton rows={2} />}
      {query.isError && <ApiErrorView error={query.error} onRetry={() => void query.refetch()} />}
      {query.isSuccess && <List page={page} onPage={setPage} items={query.data.items} hasNext={query.data.has_next} stale={query.isPlaceholderData} />}
    </section>
  )
}

interface ListProps {
  page: number
  onPage: (page: number) => void
  items: (Schemas['ExecutedStatement'] & { status?: 'started' | 'succeeded' | 'failed'; sqlstate?: string | null })[]
  hasNext: boolean
  stale: boolean
}

function List({ page, onPage, items, hasNext, stale }: ListProps) {
  if (items.length === 0 && page === 1) return <p className="small text-muted mb-0">Belum ada statement yang tercatat untuk eksekusi ini.</p>
  const offset = (page - 1) * statementsPageSize
  return (
    <>
      <ol className="statement-list list-unstyled mb-2" start={offset + 1} aria-busy={stale}>
        {items.map((statement, index) => (
          <li key={offset + index} className="mb-2">
            {statement.status && <span className="small text-muted me-2">{({ started: 'Hasil belum tercatat', succeeded: 'Statement berhasil', failed: 'Statement gagal' })[statement.status]}{statement.sqlstate ? ` · SQLSTATE ${statement.sqlstate}` : ''}</span>}
            {statement.batch !== null && <span className="badge badge-soft me-2">Batch {statement.batch}</span>}
            <pre className="code-block mono mt-1 mb-0">{statement.query}</pre>
          </li>
        ))}
      </ol>
      {(page > 1 || hasNext) && (
        <nav className="pagination-bar" aria-label="Halaman statement">
          <div>
            <button type="button" className="btn btn-sm btn-light" disabled={page === 1} onClick={() => onPage(page - 1)}>
              <i className="fas fa-chevron-left" aria-hidden="true" />
              Sebelumnya
            </button>
          </div>
          <span>
            Halaman {page} · statement {offset + 1} sampai {offset + items.length}
          </span>
          <div>
            <button type="button" className="btn btn-sm btn-light" disabled={!hasNext} onClick={() => onPage(page + 1)}>
              Berikutnya
              <i className="fas fa-chevron-right ms-2 me-0" aria-hidden="true" />
            </button>
          </div>
        </nav>
      )}
    </>
  )
}
