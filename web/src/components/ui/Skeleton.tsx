interface SkeletonProps {
  width?: string
  height?: string
  className?: string
}

export function Skeleton({ width = '100%', height = '1rem', className = '' }: SkeletonProps) {
  return <span className={`skeleton ${className}`.trim()} style={{ width, height }} aria-hidden="true" />
}

export function SkeletonRows({ rows = 5 }: { rows?: number }) {
  return (
    <div className="d-flex flex-column gap-3 p-3">
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} width={i % 2 ? '85%' : '100%'} />
      ))}
    </div>
  )
}

// Kerangka halaman standar: judul + satu box berisi baris placeholder.
export function PageSkeleton({ rows = 5 }: { rows?: number }) {
  return (
    <div role="status" aria-busy="true">
      <div className="page-header">
        <Skeleton width="6rem" height=".75rem" className="mb-2" />
        <Skeleton width="14rem" height="1.6rem" />
      </div>
      <div className="box box-primary">
        <SkeletonRows rows={rows} />
      </div>
      <span className="visually-hidden">Memuat…</span>
    </div>
  )
}
