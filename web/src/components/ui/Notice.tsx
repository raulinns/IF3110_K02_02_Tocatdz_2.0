import type { ReactNode } from 'react'

interface NoticeProps {
  kind: 'success' | 'error' | 'warning' | 'info'
  children: ReactNode
  onDismiss?: () => void
}

const appearance = {
  success: { className: 'alert-success notice-success', icon: 'fa-circle-check', role: 'status' },
  error: { className: 'alert-danger notice notice-error', icon: 'fa-circle-exclamation', role: 'alert' },
  warning: { className: 'alert-warning', icon: 'fa-triangle-exclamation', role: 'note' },
  info: { className: 'alert-info', icon: 'fa-circle-info', role: 'note' },
} as const

// Hasil sukses diumumkan sopan lewat role status, sedangkan kegagalan langsung lewat role alert
export function Notice({ kind, children, onDismiss }: NoticeProps) {
  const { className, icon, role } = appearance[kind]
  return (
    <div className={`alert ${className}`} role={role}>
      <i className={`fas ${icon}`} aria-hidden="true" />
      <div className="flex-grow-1">{children}</div>
      {onDismiss && <button type="button" className="btn-close" aria-label="Tutup pemberitahuan" onClick={onDismiss} />}
    </div>
  )
}
