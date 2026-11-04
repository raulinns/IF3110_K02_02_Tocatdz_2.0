import { Navigate, Outlet, useLocation } from 'react-router-dom'
import { ErrorPage } from '../components/ui/ErrorPage.tsx'
import { PageSkeleton } from '../components/ui/Skeleton.tsx'
import { useSession } from './session-context.ts'

function FullscreenSkeleton({ rows }: { rows?: number }) {
  return (
    <div className="fullscreen-center">
      <div className="w-100">
        <PageSkeleton rows={rows} />
      </div>
    </div>
  )
}

// Semua route aplikasi (selain /login) lewat sini. Backend tetap memeriksa akses; ini hanya UX.
export function RequireAuth() {
  const state = useSession()
  const location = useLocation()
  switch (state.status) {
    case 'loading':
      return <FullscreenSkeleton />
    case 'error':
      return <ErrorPage status={500} onRetry={state.retry} />
    case 'anonymous':
      return <Navigate to="/login" replace state={{ from: location.pathname + location.search }} />
    case 'authenticated':
      return <Outlet />
  }
}

export function RequireAdmin() {
  const state = useSession()
  if (state.status === 'authenticated' && !state.session.platform_admin) return <ErrorPage status={403} />
  return <Outlet />
}

// Halaman login tidak berguna jika sudah masuk.
export function GuestOnly() {
  const state = useSession()
  if (state.status === 'authenticated') return <Navigate to="/databases" replace />
  if (state.status === 'loading') return <FullscreenSkeleton rows={3} />
  return <Outlet />
}
