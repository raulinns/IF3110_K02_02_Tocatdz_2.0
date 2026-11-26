import { useRef, useState, type FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { createGroup } from '../../api/admin.ts'
import { isApiError } from '../../api/errors.ts'
import { errorMessage, fieldError } from '../../api/messages.ts'
import type { Schemas } from '../../api/types.ts'
import { Field } from '../../components/ui/Field.tsx'
import { Notice } from '../../components/ui/Notice.tsx'
import { nameProblem } from '../../lib/validation.ts'

interface CreateGroupFormProps {
  onCreated: (group: Schemas['Group']) => void
}

export function CreateGroupForm({ onCreated }: CreateGroupFormProps) {
  const nameRef = useRef<HTMLInputElement>(null)
  const inFlight = useRef(false)
  const [name, setName] = useState('')
  const [problem, setProblem] = useState<string>()
  const mutation = useMutation({ mutationFn: createGroup })

  const general = mutation.error && !problem ? errorMessage(mutation.error) : null

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (inFlight.current || mutation.isPending) return
    const trimmed = name.trim()
    const found = nameProblem(trimmed, 'Nama group') ?? undefined
    setProblem(found)
    if (found) {
      mutation.reset()
      nameRef.current?.focus()
      return
    }
    inFlight.current = true
    mutation.mutate(
      { name: trimmed },
      {
        onSettled: () => {
          inFlight.current = false
        },
        onSuccess: (group) => {
          setName('')
          setProblem(undefined)
          onCreated(group)
          nameRef.current?.focus()
        },
        onError: (error) => {
          if (!isApiError(error)) return
          // Konflik pada pembuatan berarti nama sudah dipakai sehingga pesannya ditentukan di sini karena teks server bersifat umum
          const message = fieldError(error, 'name') ?? (error.code === 'conflict' ? 'Nama group sudah dipakai' : undefined)
          setProblem(message)
          if (message) nameRef.current?.focus()
        },
      },
    )
  }

  return (
    <div className="card mb-0">
      <div className="card-header">
        <i className="fas fa-plus text-muted" aria-hidden="true" />
        <h2 className="h6 mb-0">Buat group</h2>
      </div>
      <div className="card-body">
        {general && (
          <div className="mb-3">
            <Notice kind="error">{general}</Notice>
          </div>
        )}
        <form onSubmit={submit} noValidate aria-busy={mutation.isPending}>
          <Field id="group-name" label="Nama group" error={problem}>
            {(control) => (
              <input
                {...control}
                name="name"
                type="text"
                ref={nameRef}
                value={name}
                onChange={(event) => {
                  setName(event.target.value)
                  setProblem(undefined)
                }}
                autoComplete="off"
              />
            )}
          </Field>
          <button type="submit" className="btn btn-primary w-100" disabled={mutation.isPending}>
            {mutation.isPending ? (
              <>
                <span className="spinner-border spinner-border-sm me-2" aria-hidden="true" />
                Membuat
              </>
            ) : (
              <>
                <i className="fas fa-plus" aria-hidden="true" />
                Buat group
              </>
            )}
          </button>
        </form>
      </div>
    </div>
  )
}
