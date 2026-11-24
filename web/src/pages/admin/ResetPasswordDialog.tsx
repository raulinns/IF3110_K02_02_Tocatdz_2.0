import { useRef, useState, type FormEvent } from 'react'
import { updateUser } from '../../api/admin.ts'
import { isApiError } from '../../api/errors.ts'
import { errorMessage, fieldError } from '../../api/messages.ts'
import type { Schemas } from '../../api/types.ts'
import { Field } from '../../components/ui/Field.tsx'
import { Modal } from '../../components/ui/Modal.tsx'
import { Notice } from '../../components/ui/Notice.tsx'
import { PasswordInput } from '../../components/ui/PasswordInput.tsx'
import { passwordProblem } from '../../lib/validation.ts'

interface ResetPasswordDialogProps {
  user: Schemas['User']
  onDone: (user: Schemas['User']) => void
  onClose: () => void
}

const formId = 'reset-password-form'

export function ResetPasswordDialog({ user, onDone, onClose }: ResetPasswordDialogProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  const inFlight = useRef(false)
  const [password, setPassword] = useState('')
  const [problem, setProblem] = useState<string>()
  const [failure, setFailure] = useState<unknown>(null)
  const [pending, setPending] = useState(false)

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (inFlight.current) return
    const found = passwordProblem(password) ?? undefined
    setProblem(found)
    setFailure(null)
    if (found) {
      inputRef.current?.focus()
      return
    }
    inFlight.current = true
    setPending(true)
    try {
      await updateUser(user.id, { password })
      onDone(user)
      onClose()
    } catch (error) {
      const message = isApiError(error) ? fieldError(error, 'password') : undefined
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
          <i className="fas fa-key" aria-hidden="true" /> Reset password
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
            Reset password
          </button>
        </>
      }
    >
      <p>
        Password baru untuk <span className="mono">{user.username}</span>. Semua sesi aktif user ini akan dicabut sehingga ia harus masuk kembali.
      </p>
      {failure !== null && (
        <div className="mb-3">
          <Notice kind="error">{errorMessage(failure)}</Notice>
        </div>
      )}
      <form id={formId} onSubmit={(event) => void submit(event)} noValidate>
        <Field id="reset-password" label="Password baru" error={problem} hint="Minimal 15 karakter" className="mb-0" feedback={false}>
          {(control) => (
            <PasswordInput
              {...control}
              inputRef={inputRef}
              value={password}
              onChange={(event) => {
                setPassword(event.target.value)
                setProblem(undefined)
              }}
              autoComplete="new-password"
              error={problem}
            />
          )}
        </Field>
      </form>
    </Modal>
  )
}
