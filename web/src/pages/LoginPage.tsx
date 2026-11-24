import { useRef, useState, type ChangeEvent, type FormEvent } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { isApiError } from '../api/errors.ts'
import { errorMessage, fieldError } from '../api/messages.ts'
import brand from '../assets/stiker-if.png'
import { login } from '../auth/login.ts'
import { startSession } from '../auth/session-cache.ts'
import { Field } from '../components/ui/Field.tsx'
import { PasswordInput } from '../components/ui/PasswordInput.tsx'
import { ThemeToggle } from '../components/ui/ThemeToggle.tsx'
import { useCountdown } from '../lib/useCountdown.ts'

type Problems = Partial<Record<'username' | 'password', string>>

const rateLimitedMessage = 'Terlalu banyak percobaan masuk. Silakan coba lagi setelah hitung mundur selesai.'

// Gagal login selalu memakai satu pesan generik supaya tidak membocorkan apakah username terdaftar
function failureMessage(error: unknown): string {
  if (isApiError(error) && error.code === 'unauthenticated') return 'Username atau password salah.'
  return errorMessage(error)
}

export function LoginPage() {
  const client = useQueryClient()
  const usernameRef = useRef<HTMLInputElement>(null)
  const passwordRef = useRef<HTMLInputElement>(null)
  const inFlight = useRef(false)
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [problems, setProblems] = useState<Problems>({})
  const countdown = useCountdown()
  const mutation = useMutation({ mutationFn: login, onSuccess: (session) => startSession(client, session) })

  const error = mutation.error
  const busy = mutation.isPending
  const locked = countdown.remaining > 0
  const limited = isApiError(error) && error.code === 'rate_limited'
  const fieldProblem = isApiError(error) && error.code === 'validation_failed' && Object.keys(error.fields).length > 0
  const alert = error && !fieldProblem && (!limited || locked) ? (limited ? rateLimitedMessage : failureMessage(error)) : null

  function edit(field: 'username' | 'password', setter: (value: string) => void) {
    return (event: ChangeEvent<HTMLInputElement>) => {
      setter(event.target.value)
      setProblems((current) => ({ ...current, [field]: undefined }))
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    // Ref dipakai karena status pending baru terlihat setelah render ulang dan klik cepat bisa lolos
    if (inFlight.current || busy || locked) return
    const missing: Problems = {}
    if (username.trim() === '') missing.username = 'Username wajib diisi'
    if (password === '') missing.password = 'Password wajib diisi'
    setProblems(missing)
    if (missing.username || missing.password) {
      mutation.reset()
      const firstInvalid = missing.username ? usernameRef : passwordRef
      firstInvalid.current?.focus()
      return
    }
    inFlight.current = true
    mutation.mutate(
      { username: username.trim(), password },
      {
        onSettled: () => {
          inFlight.current = false
        },
        onError: (err) => {
          if (!isApiError(err)) return
          if (err.code === 'rate_limited') countdown.start(err.retryAfter ?? 60)
          if (err.code === 'validation_failed') setProblems({ username: fieldError(err, 'username'), password: fieldError(err, 'password') })
          if (err.code === 'unauthenticated') {
            setPassword('')
            passwordRef.current?.focus()
          }
        },
      },
    )
  }

  return (
    <main className="auth-wrapper">
      <div className="auth-content">
        <div className="card">
          <div className="card-body text-center">
            <div className="mb-4">
              <img className="brand" src={brand} alt="IF Tidur, Besok Balik Nubes" width={180} height={180} />
            </div>
            <h1 className="h6 mb-1">Masuk ke akun Anda</h1>
            <p className="text-muted small mb-4">Platform pengelolaan database PostgreSQL dan MySQL</p>
            {alert && (
              <div className="alert alert-danger notice notice-error text-start" role="alert">
                <i className="fas fa-circle-exclamation" aria-hidden="true" />
                <div>{alert}</div>
              </div>
            )}
            <form onSubmit={submit} noValidate aria-busy={busy}>
              <Field id="username" label="Username" error={problems.username}>
                {(control) => (
                  <input
                    {...control}
                    name="username"
                    type="text"
                    ref={usernameRef}
                    value={username}
                    onChange={edit('username', setUsername)}
                    placeholder="Masukkan username"
                    autoComplete="username"
                    autoCapitalize="none"
                    spellCheck={false}
                    autoFocus
                  />
                )}
              </Field>
              <Field id="password" label="Password" error={problems.password} className="mb-4" feedback={false}>
                {(control) => (
                  <PasswordInput {...control} inputRef={passwordRef} value={password} onChange={edit('password', setPassword)} placeholder="Masukkan password" autoComplete="current-password" error={problems.password} />
                )}
              </Field>
              <button type="submit" className="btn btn-primary w-100" disabled={busy || locked}>
                {busy ? (
                  <>
                    <span className="spinner-border spinner-border-sm me-2" aria-hidden="true" />
                    Memproses
                  </>
                ) : locked ? (
                  `Coba lagi dalam ${countdown.remaining} detik`
                ) : (
                  <>
                    <i className="fas fa-right-to-bracket" aria-hidden="true" />
                    Masuk
                  </>
                )}
              </button>
            </form>
          </div>
        </div>
        <div className="auth-footer">
          <span>Akun dibuat oleh admin platform.</span>
          <ThemeToggle />
        </div>
      </div>
    </main>
  )
}
