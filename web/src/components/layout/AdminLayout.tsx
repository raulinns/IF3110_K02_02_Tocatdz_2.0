import { NavLink, Outlet } from 'react-router-dom'
import { PageHeader } from '../ui/PageHeader.tsx'

const tabs = [
  { to: 'users', icon: 'fa-user', label: 'User' },
  { to: 'groups', icon: 'fa-users', label: 'Group' },
  { to: 'connections', icon: 'fa-plug', label: 'Koneksi' },
  { to: 'grants', icon: 'fa-user-shield', label: 'Akses' },
]

export function AdminLayout() {
  return (
    <>
      <PageHeader pretitle="Platform" title="Administrasi" />
      <ul className="nav nav-tabs nav-tabs-scroll mb-3" aria-label="Bagian admin">
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
