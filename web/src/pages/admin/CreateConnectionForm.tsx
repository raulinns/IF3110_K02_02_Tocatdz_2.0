import { useRef, useState, type FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { createConnection } from '../../api/admin.ts'
import { isApiError } from '../../api/errors.ts'
import { errorMessage, fieldError } from '../../api/messages.ts'
import type { Schemas } from '../../api/types.ts'
import { Field } from '../../components/ui/Field.tsx'
import { Notice } from '../../components/ui/Notice.tsx'
import { dsnProblem, nameProblem } from '../../lib/validation.ts'
import { engineLabel } from '../../lib/labels.ts'

type Engine = Schemas['ConnectionInput']['engine']
type Problems = Partial<Record<'engine' | 'name' | 'dsn', string>>

const engines: Engine[] = ['postgresql', 'mysql']

interface CreateConnectionFormProps {
  onCreated: (connection: Schemas['Connection']) => void
}

export function CreateConnectionForm({ onCreated }: CreateConnectionFormProps) {
  const nameRef = useRef<HTMLInputElement>(null)
  const dsnRef = useRef<HTMLInputElement>(null)
  const inFlight = useRef(false)
  const [engine, setEngine] = useState<Engine>('postgresql')
  const [name, setName] = useState('')
  const [dsn, setDsn] = useState('')
  const [problems, setProblems] = useState<Problems>({})
  const mutation = useMutation({ mutationFn: createConnection })

  const failure = mutation.error
  const general = failure && !Object.values(problems).some(Boolean) ? errorMessage(failure) : null

  function clear(field: keyof Problems) {
    setProblems((current) => ({ ...current, [field]: undefined }))
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (inFlight.current || mutation.isPending) return
    const trimmed = name.trim()
    const found: Problems = { name: nameProblem(trimmed, 'Nama tampilan') ?? undefined, dsn: dsnProblem(dsn) ?? undefined }
    setProblems(found)
    if (found.name || found.dsn) {
      mutation.reset()
      ;(found.name ? nameRef : dsnRef).current?.focus()
      return
    }
    inFlight.current = true
    mutation.mutate(
      { engine, name: trimmed, dsn },
      {
        onSettled: () => {
          inFlight.current = false
        },
        onSuccess: (connection) => {
          // Connection string langsung dibuang dari memori form setelah tersimpan
          setName('')
          setDsn('')
          setProblems({})
          onCreated(connection)
          nameRef.current?.focus()
        },
        onError: (error) => {
          if (!isApiError(error)) return
          const taken = error.code === 'conflict' ? errorMessage(error) : undefined
          const next = { engine: fieldError(error, 'engine'), name: fieldError(error, 'name') ?? taken, dsn: fieldError(error, 'dsn') }
          setProblems(next)
          if (next.name) nameRef.current?.focus()
          else if (next.dsn) dsnRef.current?.focus()
        },
      },
    )
  }

  return (
    <div className="card mb-0">
      <div className="card-header">
        <i className="fas fa-plug text-muted" aria-hidden="true" />
        <h2 className="h6 mb-0">Tambah koneksi database</h2>
      </div>
      <div className="card-body">
        {general && (
          <div className="mb-3">
            <Notice kind="error">{general}</Notice>
          </div>
        )}
        <form onSubmit={submit} noValidate aria-busy={mutation.isPending}>
          <Field id="connection-engine" label="DBMS" error={problems.engine}>
            {(control) => (
              <select {...control} className={control.className.replace('form-control', 'form-select')} name="engine" value={engine} onChange={(event) => setEngine(event.target.value as Engine)}>
                {engines.map((option) => (
                  <option key={option} value={option}>
                    {engineLabel(option)}
                  </option>
                ))}
              </select>
            )}
          </Field>
          <Field id="connection-name" label="Nama tampilan" error={problems.name}>
            {(control) => (
              <input
                {...control}
                name="name"
                type="text"
                ref={nameRef}
                value={name}
                onChange={(event) => {
                  setName(event.target.value)
                  clear('name')
                }}
                autoComplete="off"
              />
            )}
          </Field>
          <Field
            id="connection-dsn"
            label="Connection string"
            error={problems.dsn}
            hint={
              <>
                <i className="fas fa-lock" aria-hidden="true" /> Disimpan terenkripsi dan tidak akan ditampilkan lagi.
              </>
            }
          >
            {(control) => (
              <input
                {...control}
                name="dsn"
                type="password"
                ref={dsnRef}
                value={dsn}
                onChange={(event) => {
                  setDsn(event.target.value)
                  clear('dsn')
                }}
                placeholder="Connection string PostgreSQL atau MySQL"
                autoComplete="off"
                spellCheck={false}
              />
            )}
          </Field>
          <button type="submit" className="btn btn-primary w-100" disabled={mutation.isPending}>
            {mutation.isPending ? (
              <>
                <span className="spinner-border spinner-border-sm me-2" aria-hidden="true" />
                Menyimpan
              </>
            ) : (
              <>
                <i className="fas fa-floppy-disk" aria-hidden="true" />
                Simpan koneksi
              </>
            )}
          </button>
        </form>
      </div>
    </div>
  )
}
