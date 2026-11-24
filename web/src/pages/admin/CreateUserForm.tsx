import { useRef, useState, type FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { createUser } from '../../api/admin.ts'
import { isApiError } from '../../api/errors.ts'
import { errorMessage, fieldError } from '../../api/messages.ts'
import type { Schemas } from '../../api/types.ts'
import { Field } from '../../components/ui/Field.tsx'
import { Notice } from '../../components/ui/Notice.tsx'
import { PasswordInput } from '../../components/ui/PasswordInput.tsx'
import { normalizeUsername, passwordProblem, usernameProblem } from '../../lib/validation.ts'

type Problems = Partial<Record<'username' | 'password', string>>

interface CreateUserFormProps {
  onCreated: (user: Schemas['User']) => void
}

export function CreateUserForm({ onCreated }: CreateUserFormProps) {
  const usernameRef = useRef<HTMLInputElement>(null)
  const passwordRef = useRef<HTMLInputElement>(null)
  const inFlight = useRef(false)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [platformAdmin, setPlatformAdmin] = useState(false)
  const [problems, setProblems] = useState<Problems>({})
  const mutation = useMutation({ mutationFn: createUser })

  const failure = mutation.error
  // Error yang sudah menempel di sebuah field tidak diulang sebagai notifikasi
  const general = failure && !Object.values(problems).some(Boolean) ? errorMessage(failure) : null

  function edit(field: keyof Problems, setter: (value: string) => void) {
    return (value: string) => {
      setter(value)
      setProblems((current) => ({ ...current, [field]: undefined }))
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    // Ref dipakai karena status pending baru terlihat setelah render ulang dan klik cepat bisa lolos
    if (inFlight.current || mutation.isPending) return
    const normalized = normalizeUsername(username)
    const found: Problems = { username: usernameProblem(normalized) ?? undefined, password: passwordProblem(password) ?? undefined }
    setProblems(found)
    if (found.username || found.password) {
      mutation.reset()
      ;(found.username ? usernameRef : passwordRef).current?.focus()
      return
    }
    inFlight.current = true
    mutation.mutate(
      { username: normalized, password, platform_admin: platformAdmin },
      {
        onSettled: () => {
          inFlight.current = false
        },
        onSuccess: (user) => {
          setUsername('')
          setPassword('')
          setPlatformAdmin(false)
          setProblems({})
          onCreated(user)
          usernameRef.current?.focus()
        },
        onError: (error) => {
          if (!isApiError(error)) return
          // Username yang sudah dipakai dikembalikan server sebagai konflik, sehingga ditampilkan di field-nya
          const taken = error.code === 'conflict' ? errorMessage(error) : undefined
          const next = { username: fieldError(error, 'username') ?? taken, password: fieldError(error, 'password') }
          setProblems(next)
          if (next.username) usernameRef.current?.focus()
          else if (next.password) passwordRef.current?.focus()
        },
      },
    )
  }

  return (
    <div className="card mb-0">
      <div className="card-header">
        <i className="fas fa-user-plus text-muted" aria-hidden="true" />
        <h2 className="h6 mb-0">Tambah user</h2>
      </div>
      <div className="card-body">
        {general && (
          <div className="mb-3">
            <Notice kind="error">{general}</Notice>
          </div>
        )}
        <form onSubmit={submit} noValidate aria-busy={mutation.isPending}>
          <Field id="new-username" label="Username" error={problems.username} hint="Huruf kecil, angka, dan garis bawah, 3-32 karakter">
            {(control) => (
              <input
                {...control}
                name="username"
                type="text"
                ref={usernameRef}
                value={username}
                onChange={(event) => edit('username', setUsername)(event.target.value)}
                autoComplete="off"
                autoCapitalize="none"
                spellCheck={false}
              />
            )}
          </Field>
          <Field id="new-password" label="Password awal" error={problems.password} hint="Minimal 15 karakter" feedback={false}>
            {(control) => (
              <PasswordInput {...control} inputRef={passwordRef} value={password} onChange={(event) => edit('password', setPassword)(event.target.value)} autoComplete="new-password" error={problems.password} />
            )}
          </Field>
          <div className="form-check mb-3">
            <input id="new-admin" className="form-check-input" name="platform_admin" type="checkbox" checked={platformAdmin} onChange={(event) => setPlatformAdmin(event.target.checked)} />
            <label className="form-check-label" htmlFor="new-admin">
              Jadikan admin platform
            </label>
          </div>
          <button type="submit" className="btn btn-primary w-100" disabled={mutation.isPending}>
            {mutation.isPending ? (
              <>
                <span className="spinner-border spinner-border-sm me-2" aria-hidden="true" />
                Memproses
              </>
            ) : (
              <>
                <i className="fas fa-user-plus" aria-hidden="true" />
                Tambah user
              </>
            )}
          </button>
        </form>
      </div>
    </div>
  )
}
