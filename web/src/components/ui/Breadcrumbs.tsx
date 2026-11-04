import { Link } from 'react-router-dom'

export interface Crumb {
  label: string
  to?: string
}

export function Breadcrumbs({ items }: { items: Crumb[] }) {
  return (
    <nav aria-label="Breadcrumb">
      <ol className="breadcrumb">
        {items.map((item, i) => {
          const last = i === items.length - 1
          return (
            <li key={`${i}-${item.label}`} className={`breadcrumb-item${last ? ' active' : ''}`} aria-current={last ? 'page' : undefined}>
              {item.to && !last ? <Link to={item.to}>{item.label}</Link> : item.label}
            </li>
          )
        })}
      </ol>
    </nav>
  )
}
