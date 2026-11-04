import { useEffect, useState } from 'react'
import { Outlet } from 'react-router-dom'
import { useSession } from '../../auth/session-context.ts'
import { useMediaQuery } from '../../lib/useMediaQuery.ts'
import { Sidebar } from './Sidebar.tsx'
import { Topbar } from './Topbar.tsx'

// Breakpoint sama dengan @media di app.css: di bawahnya sidebar menjadi offcanvas (default tertutup).
const mobileQuery = '(max-width: 768px)'

export function AppLayout() {
  const state = useSession()
  const mobile = useMediaQuery(mobileQuery)
  const [desktopCollapsed, setDesktopCollapsed] = useState(false)
  const [mobileOpen, setMobileOpen] = useState(false)

  const open = mobile ? mobileOpen : !desktopCollapsed
  // Di desktop `.toggled` menyembunyikan sidebar; di mobile `.toggled` menampilkannya (lihat app.css).
  const toggled = mobile ? mobileOpen : desktopCollapsed
  const offcanvas = mobile && mobileOpen

  useEffect(() => {
    if (!offcanvas) return
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape') setMobileOpen(false)
    }
    document.addEventListener('keydown', onKey)
    document.body.style.overflow = 'hidden'
    return () => {
      document.removeEventListener('keydown', onKey)
      document.body.style.overflow = ''
    }
  }, [offcanvas])

  if (state.status !== 'authenticated') return null
  const { session, logout } = state

  return (
    <div className="wrapper">
      <Sidebar admin={session.platform_admin} toggled={toggled} hidden={!open} onNavigate={() => setMobileOpen(false)} />
      {offcanvas && <div className="sidebar-backdrop" onClick={() => setMobileOpen(false)} aria-hidden="true" />}
      <div id="body">
        <Topbar
          username={session.user.username}
          admin={session.platform_admin}
          menuOpen={open}
          onToggleMenu={() => (mobile ? setMobileOpen((v) => !v) : setDesktopCollapsed((v) => !v))}
          onLogout={logout}
        />
        <main className="content">
          <div className="container-fluid">
            <Outlet />
          </div>
        </main>
      </div>
    </div>
  )
}
