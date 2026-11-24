import { useState, type ReactNode } from 'react'
import { errorMessage } from '../../api/messages.ts'
import { Modal } from './Modal.tsx'

interface ConfirmDialogProps {
  title: string
  children: ReactNode
  confirmLabel: string
  danger?: boolean
  onConfirm: () => Promise<unknown>
  onClose: () => void
}

// Dialog tetap terbuka dengan pesan error jika aksinya gagal sehingga pengguna bisa mencoba lagi atau membatalkan
export function ConfirmDialog({ title, children, confirmLabel, danger = false, onConfirm, onClose }: ConfirmDialogProps) {
  const [pending, setPending] = useState(false)
  const [error, setError] = useState<unknown>(null)

  async function confirm() {
    setPending(true)
    setError(null)
    try {
      await onConfirm()
      onClose()
    } catch (err) {
      setError(err)
      setPending(false)
    }
  }

  return (
    <Modal
      title={
        <>
          <i className="fas fa-triangle-exclamation" aria-hidden="true" /> {title}
        </>
      }
      onClose={onClose}
      busy={pending}
      footer={
        <>
          <button type="button" className="btn btn-light" data-autofocus disabled={pending} onClick={onClose}>
            Batal
          </button>
          <button type="button" className={`btn ${danger ? 'btn-danger' : 'btn-primary'}`} disabled={pending} onClick={() => void confirm()}>
            {pending && <span className="spinner-border spinner-border-sm me-2" aria-hidden="true" />}
            {confirmLabel}
          </button>
        </>
      }
    >
      <div>{children}</div>
      {error !== null && (
        <div className="alert alert-danger notice notice-error mt-3 mb-0" role="alert">
          <i className="fas fa-circle-exclamation" aria-hidden="true" />
          <div>{errorMessage(error)}</div>
        </div>
      )}
    </Modal>
  )
}
