import { isApiError } from '../../api/errors.ts'
import { errorMessage } from '../../api/messages.ts'
import { ErrorPage } from './ErrorPage.tsx'

interface ApiErrorViewProps {
  error: unknown
  onRetry?: () => void
}

// 403/404 tampil sebagai halaman penuh; 401 tidak ditampilkan karena pengguna sedang dialihkan ke login.
export function ApiErrorView({ error, onRetry }: ApiErrorViewProps) {
  if (isApiError(error)) {
    if (error.status === 401) return null
    if (error.status === 403 || error.status === 404) return <ErrorPage status={error.status} />
  }
  const requestId = isApiError(error) ? error.requestId : undefined
  return (
    <div className="alert alert-danger notice notice-error" role="alert">
      <i className="fas fa-circle-exclamation" aria-hidden="true" />
      <div className="flex-grow-1">
        <div>{errorMessage(error)}</div>
        {requestId && <div className="small text-muted mono">ID permintaan: {requestId}</div>}
      </div>
      {onRetry && (
        <button type="button" className="btn btn-sm btn-outline-secondary" onClick={onRetry}>
          <i className="fas fa-rotate-right" aria-hidden="true" />
          Coba lagi
        </button>
      )}
    </div>
  )
}
