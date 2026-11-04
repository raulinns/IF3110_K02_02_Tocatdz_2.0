import { useState } from 'react'
import { useTheme } from '../../lib/theme.ts'

interface TopbarProps {
  username: string
  admin: boolean
  menuOpen: boolean
  onToggleMenu: () => void
  onLogout: () => Promise<void>
}

export function Topbar({ username, admin, menuOpen, onToggleMenu, onLogout }: TopbarProps) {
  const { theme, toggle } = useTheme()
  const [leaving, setLeaving] = useState(false)
  const dark = theme === 'dark'

  async function logout() {
    setLeaving(true)
    try {
      await onLogout()
    } finally {
      setLeaving(false)
    }
  }

  return (
    <header className="topbar">
      <button
        type="button"
        className="btn btn-light btn-icon"
        aria-controls="sidebar"
        aria-expanded={menuOpen}
        aria-label="Tampilkan atau sembunyikan menu"
        onClick={onToggleMenu}
      >
        <i className="fas fa-bars" aria-hidden="true" />
      </button>
      <div className="topbar-user">
        <button
          type="button"
          className="btn btn-light btn-icon"
          aria-pressed={dark}
          title={dark ? 'Ganti ke mode terang' : 'Ganti ke mode gelap'}
          onClick={toggle}
        >
          <i className="fas fa-moon theme-icon-dark" aria-hidden="true" />
          <i className="fas fa-sun theme-icon-light" aria-hidden="true" />
          <span className="visually-hidden">Ganti tema gelap atau terang</span>
        </button>
        <span className="username">
          <i className="fas fa-circle-user" aria-hidden="true" /> <span className="name mono">{username}</span>
          {admin && <span className="badge access-view_edit ms-1">Admin</span>}
        </span>
        <button type="button" className="btn btn-sm btn-outline-secondary" disabled={leaving} onClick={logout}>
          <i className="fas fa-right-from-bracket" aria-hidden="true" />
          <span className="btn-label">Keluar</span>
        </button>
      </div>
    </header>
  )
}
