import type { ReactNode } from 'react'
import { Breadcrumbs, type Crumb } from './Breadcrumbs.tsx'

interface PageHeaderProps {
  pretitle?: string
  title: ReactNode
  breadcrumbs?: Crumb[]
}

export function PageHeader({ pretitle, title, breadcrumbs }: PageHeaderProps) {
  return (
    <div className="page-header">
      {breadcrumbs ? <Breadcrumbs items={breadcrumbs} /> : pretitle ? <div className="page-pretitle">{pretitle}</div> : null}
      <h1 className="page-title">{title}</h1>
    </div>
  )
}
