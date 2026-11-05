import { NavLink, Outlet, useParams } from 'react-router-dom'
import { PageHeader } from '../ui/PageHeader.tsx'

const tabs = [
  { to: 'schema', icon: 'fa-table', label: 'Schema' },
  { to: 'migrations', icon: 'fa-code-branch', label: 'Migration' },
  { to: 'schema-diff', icon: 'fa-code-compare', label: 'Schema diff' },
  { to: 'review', icon: 'fa-clipboard-check', label: 'Review' },
  { to: 'jobs', icon: 'fa-gauge-high', label: 'Monitor' },
  { to: 'history', icon: 'fa-clock-rotate-left', label: 'Riwayat' },
]

// Kerangka semua halaman di bawah /databases/:db: breadcrumb, judul, dan tab bagian database.
export function DatabaseLayout() {
  const { db = '' } = useParams()
  return (
    <>
      <PageHeader
        breadcrumbs={[{ label: 'Database', to: '/databases' }, { label: db }]}
        title={<span className="mono">{db}</span>}
      />
      <ul className="nav nav-tabs nav-tabs-scroll mb-3" aria-label="Bagian database">
        {tabs.map((tab) => (
          <li className="nav-item" key={tab.to}>
            <NavLink className="nav-link" to={tab.to}>
              <i className={`fas ${tab.icon}`} aria-hidden="true" /> {tab.label}
            </NavLink>
          </li>
        ))}
      </ul>
      <Outlet />
    </>
  )
}
