import { useRef, useState, type FormEvent } from 'react'
import { renameGroup } from '../../api/admin.ts'
import { isApiError } from '../../api/errors.ts'
import { errorMessage, fieldError } from '../../api/messages.ts'
import type { Schemas } from '../../api/types.ts'
import { Field } from '../../components/ui/Field.tsx'
import { Modal } from '../../components/ui/Modal.tsx'
import { Notice } from '../../components/ui/Notice.tsx'
import { nameProblem } from '../../lib/validation.ts'

interface RenameGroupDialogProps {
  group: Schemas['Group']
  onDone: (group: Schemas['Group']) => void
  onClose: () => void
}

const formId = 'rename-group-form'

export function RenameGroupDialog({ group, onDone, onClose }: RenameGroupDialogProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  const inFlight = useRef(false)
  const [name, setName] = useState(group.name)
  const [problem, setProblem] = useState<string>()
  const [failure, setFailure] = useState<unknown>(null)
  const [pending, setPending] = useState(false)

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (inFlight.current) return
    const trimmed = name.trim()
    const found = nameProblem(trimmed, 'Nama group') ?? undefined
    setProblem(found)
    setFailure(null)
    if (found) {
      inputRef.current?.focus()
      return
    }
    if (trimmed === group.name) {
      onClose()
      return
    }
    inFlight.current = true
    setPending(true)
    try {
      const updated = await renameGroup(group.id, { name: trimmed })
      onDone(updated)
      onClose()
    } catch (error) {
      const message = isApiError(error) ? (fieldError(error, 'name') ?? (error.code === 'conflict' ? 'Nama group sudah dipakai' : undefined)) : undefined
      setProblem(message)
      setFailure(message ? null : error)
      setPending(false)
      inFlight.current = false
      inputRef.current?.focus()
    }
  }

  return (
    <Modal
      title={
        <>
          <i className="fas fa-pen" aria-hidden="true" /> Ganti nama group
        </>
      }
      onClose={onClose}
      busy={pending}
      footer={
        <>
          <button type="button" className="btn btn-light" disabled={pending} onClick={onClose}>
            Batal
          </button>
          <button type="submit" form={formId} className="btn btn-primary" disabled={pending}>
            {pending && <span className="spinner-border spinner-border-sm me-2" aria-hidden="true" />}
            Simpan nama
          </button>
        </>
      }
    >
      {failure !== null && (
        <div className="mb-3">
          <Notice kind="error">{errorMessage(failure)}</Notice>
        </div>
      )}
      <form id={formId} onSubmit={(event) => void submit(event)} noValidate>
        <Field id="rename-group" label="Nama group" error={problem} className="mb-0">
          {(control) => (
            <input
              {...control}
              name="name"
              type="text"
              ref={inputRef}
              value={name}
              onChange={(event) => {
                setName(event.target.value)
                setProblem(undefined)
              }}
              autoComplete="off"
            />
          )}
        </Field>
      </form>
    </Modal>
  )
}
