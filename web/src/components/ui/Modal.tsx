import { useEffect, useId, useRef, useState, type KeyboardEvent, type MouseEvent, type ReactNode } from 'react'
import { createPortal } from 'react-dom'

const focusable = 'a[href], button:not([disabled]), input:not([disabled]):not([type="hidden"]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'

interface ModalProps {
  title: ReactNode
  onClose: () => void
  // Selama proses berjalan dialog tidak boleh ditutup lewat Esc, klik latar, maupun tombol tutup
  busy?: boolean
  footer?: ReactNode
  children: ReactNode
}

// Dialog modal dengan fokus terkunci di dalamnya dan fokus kembali ke pemicunya saat ditutup
export function Modal({ title, onClose, busy = false, footer, children }: ModalProps) {
  const titleId = useId()
  const dialog = useRef<HTMLDivElement>(null)
  const [host] = useState(() => document.createElement('div'))

  useEffect(() => {
    const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null
    // Isi halaman di belakang dinonaktifkan untuk pembaca layar dan keyboard selama dialog terbuka
    const background = Array.from(document.body.children).filter((element) => element !== host && !element.hasAttribute('inert'))
    document.body.appendChild(host)
    background.forEach((element) => element.setAttribute('inert', ''))
    document.body.classList.add('modal-open')
    // Fokus awal ke elemen bertanda data-autofocus, lalu isian pertama di badan dialog, dan terakhir dialog itu sendiri
    const target = dialog.current?.querySelector<HTMLElement>('[data-autofocus]') ?? dialog.current?.querySelector('.modal-body')?.querySelector<HTMLElement>(focusable) ?? dialog.current
    target?.focus()
    return () => {
      background.forEach((element) => element.removeAttribute('inert'))
      document.body.classList.remove('modal-open')
      host.remove()
      if (opener?.isConnected) opener.focus()
    }
  }, [host])

  function handleKeyDown(event: KeyboardEvent<HTMLDivElement>) {
    if (event.key === 'Escape') {
      event.stopPropagation()
      if (!busy) onClose()
      return
    }
    if (event.key !== 'Tab') return
    const items = Array.from(dialog.current?.querySelectorAll<HTMLElement>(focusable) ?? [])
    if (items.length === 0) {
      event.preventDefault()
      return
    }
    const first = items[0]
    const last = items[items.length - 1]
    const active = document.activeElement
    if (event.shiftKey && (active === first || active === dialog.current)) {
      event.preventDefault()
      last.focus()
    } else if (!event.shiftKey && active === last) {
      event.preventDefault()
      first.focus()
    }
  }

  // Mousedown dipakai supaya menyeret seleksi teks dari dalam dialog ke latar tidak ikut menutupnya
  function handleBackdrop(event: MouseEvent<HTMLDivElement>) {
    if (event.target === event.currentTarget && !busy) onClose()
  }

  return createPortal(
    <>
      <div className="modal-backdrop show" />
      <div className="modal show d-block" onMouseDown={handleBackdrop} onKeyDown={handleKeyDown}>
        <div className="modal-dialog modal-dialog-centered modal-dialog-scrollable">
          <div className="modal-content" role="dialog" aria-modal="true" aria-labelledby={titleId} tabIndex={-1} ref={dialog}>
            <div className="modal-header">
              <h2 className="modal-title h5" id={titleId}>
                {title}
              </h2>
              <button type="button" className="btn-close" aria-label="Tutup" disabled={busy} onClick={onClose} />
            </div>
            <div className="modal-body">{children}</div>
            {footer && <div className="modal-footer">{footer}</div>}
          </div>
        </div>
      </div>
    </>,
    host,
  )
}
