import { Navigate, Outlet, useLocation } from 'react-router-dom'
import { ApiErrorView } from '../components/ui/ApiErrorView.tsx'
import { ErrorPage } from '../components/ui/ErrorPage.tsx'
import { PageSkeleton } from '../components/ui/Skeleton.tsx'
import { safeRedirectPath } from '../lib/redirect.ts'
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

// Semua route aplikasi selain /login lewat sini dan pemeriksaan akses yang sebenarnya tetap di backend
export function RequireAuth() {
  const state = useSession()
  const location = useLocation()
  switch (state.status) {
    case 'loading':
      return <FullscreenSkeleton />
    case 'error':
      return (
        <div className="fullscreen-center">
          <div className="w-100" style={{ maxWidth: 560 }}>
            <ApiErrorView error={state.error} onRetry={state.retry} />
          </div>
        </div>
      )
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

// Halaman login tidak berguna jika sudah masuk jadi pengguna dikembalikan ke halaman yang tadi dituju
export function GuestOnly() {
  const state = useSession()
  const location = useLocation()
  if (state.status === 'authenticated') return <Navigate to={safeRedirectPath((location.state as { from?: unknown } | null)?.from)} replace />
  if (state.status === 'loading') return <FullscreenSkeleton rows={3} />
  return <Outlet />
}
