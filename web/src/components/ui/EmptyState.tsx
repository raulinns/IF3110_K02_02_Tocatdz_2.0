import type { ReactNode } from 'react'

interface EmptyStateProps {
  icon?: string
  children: ReactNode
}

export function EmptyState({ icon = 'fa-inbox', children }: EmptyStateProps) {
  return (
    <div className="box">
      <div className="empty-state">
        <i className={`fas ${icon}`} aria-hidden="true" />
        <p className="mb-0">{children}</p>
      </div>
    </div>
  )
}
