import type { ReactNode } from 'react'

interface NoticeProps {
  kind: 'success' | 'error'
  children: ReactNode
  onDismiss?: () => void
}

// Hasil sukses diumumkan sopan lewat role status, sedangkan kegagalan langsung lewat role alert
export function Notice({ kind, children, onDismiss }: NoticeProps) {
  const success = kind === 'success'
  return (
    <div className={`alert ${success ? 'alert-success notice-success' : 'alert-danger notice notice-error'}`} role={success ? 'status' : 'alert'}>
      <i className={`fas ${success ? 'fa-circle-check' : 'fa-circle-exclamation'}`} aria-hidden="true" />
      <div className="flex-grow-1">{children}</div>
      {onDismiss && <button type="button" className="btn-close" aria-label="Tutup pemberitahuan" onClick={onDismiss} />}
    </div>
  )
}
