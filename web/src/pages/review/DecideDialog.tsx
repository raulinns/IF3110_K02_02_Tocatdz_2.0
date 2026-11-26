import { useRef, useState } from 'react'
import { decide } from '../../api/workflow.ts'
import { isApiError, isStaleConflict } from '../../api/errors.ts'
import { errorMessage, fieldError } from '../../api/messages.ts'
import type { Schemas } from '../../api/types.ts'
import { Field } from '../../components/ui/Field.tsx'
import { Modal } from '../../components/ui/Modal.tsx'
import { Notice } from '../../components/ui/Notice.tsx'

const maxNote = 1000
const staleReasons = new Set(['revision_mismatch', 'stale_approval'])

interface DecideDialogProps {
  db: string
  request: Schemas['ChangeRequest']
  approval: Schemas['Approval']
  onDecided: (decision: Schemas['Decision']) => void
  // Dipanggil saat permintaan berubah di server sehingga data perlu dimuat ulang
  onReload: () => void
  onClose: () => void
}

export function DecideDialog({ db, request, approval, onDecided, onReload, onClose }: DecideDialogProps) {
  const noteRef = useRef<HTMLTextAreaElement>(null)
  const [note, setNote] = useState('')
  const [problem, setProblem] = useState<string>()
  const [failure, setFailure] = useState<unknown>(null)
  const [pending, setPending] = useState<Schemas['Decision'] | null>(null)

  async function submit(decision: Schemas['Decision']) {
    if (Array.from(note).length > maxNote) {
      setProblem(`Catatan maksimal ${maxNote} karakter`)
      noteRef.current?.focus()
      return
    }
    setPending(decision)
    setProblem(undefined)
    setFailure(null)
    try {
      await decide(db, request.id, { revision: request.revision, group_id: approval.group_id, decision, note: note.trim() })
      onDecided(decision)
      onClose()
    } catch (error) {
      const message = isApiError(error) ? fieldError(error, 'note') : undefined
      setProblem(message)
      setFailure(message ? null : error)
      setPending(null)
    }
  }

  const stale = isStaleConflict(failure, staleReasons)
  const busy = pending !== null

  return (
    <Modal
      title={
        <>
          <i className="fas fa-gavel" aria-hidden="true" /> Keputusan untuk group {approval.group_name}
        </>
      }
      onClose={onClose}
      busy={busy}
      footer={
        <>
          <button type="button" className="btn btn-light" disabled={busy} onClick={onClose}>
            Batal
          </button>
          <button type="button" className="btn btn-danger" disabled={busy} onClick={() => void submit('reject')}>
            {pending === 'reject' && <span className="spinner-border spinner-border-sm me-2" aria-hidden="true" />}
            Tolak
          </button>
          <button type="button" className="btn btn-primary" disabled={busy} onClick={() => void submit('approve')}>
            {pending === 'approve' && <span className="spinner-border spinner-border-sm me-2" aria-hidden="true" />}
            Setujui
          </button>
        </>
      }
    >
      <p>
        Anda memutuskan atas nama group <strong>{approval.group_name}</strong>. Keputusan tidak bisa diubah setelah tersimpan. Penolakan dari satu group membuat seluruh permintaan ditolak.
      </p>
      {failure !== null && (
        <div className="mb-3">
          <Notice kind="error">
            <div>{errorMessage(failure)}</div>
            {stale && (
              <button
                type="button"
                className="btn btn-sm btn-outline-secondary mt-2"
                onClick={() => {
                  onReload()
                  onClose()
                }}
              >
                <i className="fas fa-rotate-right" aria-hidden="true" />
                Muat ulang permintaan
              </button>
            )}
          </Notice>
        </div>
      )}
      <Field id="decision-note" label="Catatan (opsional)" error={problem} hint={`Maksimal ${maxNote} karakter. Catatan terlihat oleh semua pembaca permintaan ini.`} className="mb-0">
        {(control) => <textarea {...control} ref={noteRef} name="note" rows={4} value={note} onChange={(event) => setNote(event.target.value)} />}
      </Field>
    </Modal>
  )
}
