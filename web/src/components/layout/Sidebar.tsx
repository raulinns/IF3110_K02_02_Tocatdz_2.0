import { Link, NavLink } from 'react-router-dom'
import logo from '../../assets/stiker-if.png'

interface SidebarProps {
  admin: boolean
  toggled: boolean
  hidden: boolean
  onNavigate: () => void
}

export function Sidebar({ admin, toggled, hidden, onNavigate }: SidebarProps) {
  return (
    <nav id="sidebar" className={toggled ? 'toggled' : undefined} aria-label="Navigasi utama" inert={hidden}>
      <div className="sidebar-header">
        <Link to="/databases" aria-label="Beranda" onClick={onNavigate}>
          <img src={logo} alt="IF Tidur, Besok Balik Nubes" className="app-logo" width={96} height={96} />
        </Link>
      </div>
      <ul className="list-unstyled components">
        <li>
          <NavLink to="/databases" onClick={onNavigate}>
            <i className="fas fa-database" aria-hidden="true" /> Database
          </NavLink>
        </li>
        {admin && (
          <>
            <li className="sidebar-heading">Administrasi</li>
            <li>
              <NavLink to="/admin/users" onClick={onNavigate}>
                <i className="fas fa-user" aria-hidden="true" /> User
              </NavLink>
            </li>
            <li>
              <NavLink to="/admin/groups" onClick={onNavigate}>
                <i className="fas fa-users" aria-hidden="true" /> Group
              </NavLink>
            </li>
            <li>
              <NavLink to="/admin/connections" onClick={onNavigate}>
                <i className="fas fa-plug" aria-hidden="true" /> Koneksi
              </NavLink>
            </li>
            <li>
              <NavLink to="/admin/grants" onClick={onNavigate}>
                <i className="fas fa-user-shield" aria-hidden="true" /> Akses
              </NavLink>
            </li>
          </>
        )}
      </ul>
    </nav>
  )
}
