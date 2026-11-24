import type { ReactNode } from 'react'
import { Link } from 'react-router-dom'

function PageButton({ to, children }: { to: string | null; children: ReactNode }) {
  if (to === null) {
    return (
      <button type="button" className="btn btn-sm btn-light" disabled>
        {children}
      </button>
    )
  }
  return (
    <Link className="btn btn-sm btn-light" to={{ search: to }}>
      {children}
    </Link>
  )
}

interface PagerProps {
  page: number
  hasNext: boolean
  // Query string untuk halaman tertentu, sehingga pengaturan lain di URL tetap terbawa
  searchFor: (page: number) => string
  info: ReactNode
  label?: string
}

export function Pager({ page, hasNext, searchFor, info, label = 'Halaman data' }: PagerProps) {
  return (
    <nav className="pagination-bar" aria-label={label}>
      <div>
        <PageButton to={page > 1 ? searchFor(page - 1) : null}>
          <i className="fas fa-chevron-left" aria-hidden="true" />
          Sebelumnya
        </PageButton>
      </div>
      <span>{info}</span>
      <div>
        <PageButton to={hasNext ? searchFor(page + 1) : null}>
          Berikutnya
          <i className="fas fa-chevron-right ms-2 me-0" aria-hidden="true" />
        </PageButton>
      </div>
    </nav>
  )
}
