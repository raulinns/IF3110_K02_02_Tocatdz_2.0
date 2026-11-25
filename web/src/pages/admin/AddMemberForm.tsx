import { useId, useRef, useState, type FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { addMember } from '../../api/admin.ts'
import { errorMessage } from '../../api/messages.ts'
import type { Schemas } from '../../api/types.ts'
import { Field } from '../../components/ui/Field.tsx'
import { Notice } from '../../components/ui/Notice.tsx'
import { normalizeUsername } from '../../lib/validation.ts'

type User = Schemas['User']

interface AddMemberFormProps {
  groupId: string
  users: User[]
  memberIds: ReadonlySet<string>
  onAdded: (user: User) => void
}

export function AddMemberForm({ groupId, users, memberIds, onAdded }: AddMemberFormProps) {
  const inputRef = useRef<HTMLInputElement>(null)
  const inFlight = useRef(false)
  const listId = useId()
  const [username, setUsername] = useState('')
  const [problem, setProblem] = useState<string>()
  const mutation = useMutation({ mutationFn: (user: User) => addMember(groupId, user.id) })

  const general = mutation.error && !problem ? errorMessage(mutation.error) : null
  const candidates = users.filter((user) => !memberIds.has(user.id))

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (inFlight.current || mutation.isPending) return
    const normalized = normalizeUsername(username)
    const user = users.find((candidate) => candidate.username === normalized)
    const found = normalized === '' ? 'Username wajib diisi' : !user ? 'User dengan username ini tidak ditemukan' : memberIds.has(user.id) ? 'User sudah menjadi anggota group ini' : undefined
    setProblem(found)
    if (found || !user) {
      mutation.reset()
      inputRef.current?.focus()
      return
    }
    inFlight.current = true
    mutation.mutate(user, {
      onSettled: () => {
        inFlight.current = false
      },
      onSuccess: () => {
        setUsername('')
        onAdded(user)
        inputRef.current?.focus()
      },
    })
  }

  return (
    <div className="card mb-0">
      <div className="card-header">
        <i className="fas fa-user-plus text-muted" aria-hidden="true" />
        <h2 className="h6 mb-0">Tambah anggota</h2>
      </div>
      <div className="card-body">
        {general && (
          <div className="mb-3">
            <Notice kind="error">{general}</Notice>
          </div>
        )}
        <form onSubmit={submit} noValidate aria-busy={mutation.isPending}>
          <Field id="member-username" label="Username" error={problem} hint="Ketik atau pilih dari daftar user">
            {(control) => (
              <input
                {...control}
                name="username"
                type="text"
                list={listId}
                ref={inputRef}
                value={username}
                onChange={(event) => {
                  setUsername(event.target.value)
                  setProblem(undefined)
                }}
                autoComplete="off"
                autoCapitalize="none"
                spellCheck={false}
              />
            )}
          </Field>
          <datalist id={listId}>
            {candidates.map((user) => (
              <option key={user.id} value={user.username} />
            ))}
          </datalist>
          <button type="submit" className="btn btn-primary w-100" disabled={mutation.isPending}>
            {mutation.isPending ? (
              <>
                <span className="spinner-border spinner-border-sm me-2" aria-hidden="true" />
                Menambahkan
              </>
            ) : (
              <>
                <i className="fas fa-user-plus" aria-hidden="true" />
                Tambahkan
              </>
            )}
          </button>
        </form>
      </div>
    </div>
  )
}
