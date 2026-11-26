import { useState } from 'react'
import { ThemeToggle } from '../ui/ThemeToggle.tsx'

interface TopbarProps {
  username: string
  admin: boolean
  menuOpen: boolean
  onToggleMenu: () => void
  onLogout: () => Promise<void>
}

export function Topbar({ username, admin, menuOpen, onToggleMenu, onLogout }: TopbarProps) {
  const [leaving, setLeaving] = useState(false)

  async function logout() {
    setLeaving(true)
    try {
      await onLogout()
    } finally {
      setLeaving(false)
    }
  }

  // Teks tombol keluar disembunyikan di layar kecil sehingga nama aksesibelnya diberikan lewat aria-label
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
        <ThemeToggle />
        <span className="username">
          <i className="fas fa-circle-user" aria-hidden="true" /> <span className="name mono">{username}</span>
          {admin && <span className="badge access-view_edit ms-1">Admin</span>}
        </span>
        <button type="button" className="btn btn-sm btn-outline-secondary" aria-label="Keluar" disabled={leaving} onClick={logout}>
          <i className="fas fa-right-from-bracket" aria-hidden="true" />
          <span className="btn-label">Keluar</span>
        </button>
      </div>
    </header>
  )
}
