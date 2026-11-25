import { useEffect, useRef, useState, type FormEvent } from 'react'
import { useMutation } from '@tanstack/react-query'
import { saveGrant } from '../../api/admin.ts'
import { isApiError } from '../../api/errors.ts'
import { errorMessage, fieldError } from '../../api/messages.ts'
import type { Schemas } from '../../api/types.ts'
import { Field } from '../../components/ui/Field.tsx'
import { Notice } from '../../components/ui/Notice.tsx'

type Access = Schemas['Access']
type Problems = Partial<Record<'group' | 'database' | 'access', string>>

export interface GrantPreset {
  groupId: string
  databaseId: string
  access: Access
}

interface GrantFormProps {
  groups: Schemas['Group'][]
  connections: Schemas['Connection'][]
  // Nilai awal dari tombol Ubah pada tabel, form dibuat ulang lewat key saat nilainya berganti
  preset?: GrantPreset
  onSaved: (grant: Schemas['Grant']) => void
}

export function GrantForm({ groups, connections, preset, onSaved }: GrantFormProps) {
  const groupRef = useRef<HTMLSelectElement>(null)
  const databaseRef = useRef<HTMLSelectElement>(null)
  const accessRef = useRef<HTMLSelectElement>(null)
  const inFlight = useRef(false)
  const [groupId, setGroupId] = useState(preset?.groupId ?? '')
  const [databaseId, setDatabaseId] = useState(preset?.databaseId ?? '')
  const [access, setAccess] = useState<Access>(preset?.access ?? 'view_only')
  const [problems, setProblems] = useState<Problems>({})
  const mutation = useMutation({ mutationFn: () => saveGrant(groupId, databaseId, { access }) })

  const general = mutation.error && !Object.values(problems).some(Boolean) ? errorMessage(mutation.error) : null
  const empty = groups.length === 0 || connections.length === 0

  useEffect(() => {
    if (preset) accessRef.current?.focus()
  }, [preset])

  function clear(field: keyof Problems) {
    setProblems((current) => ({ ...current, [field]: undefined }))
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    if (inFlight.current || mutation.isPending) return
    const found: Problems = { group: groupId ? undefined : 'Pilih group', database: databaseId ? undefined : 'Pilih database' }
    setProblems(found)
    if (found.group || found.database) {
      mutation.reset()
      ;(found.group ? groupRef : databaseRef).current?.focus()
      return
    }
    inFlight.current = true
    mutation.mutate(undefined, {
      onSettled: () => {
        inFlight.current = false
      },
      onSuccess: (grant) => onSaved(grant),
      onError: (error) => {
        if (!isApiError(error)) return
        setProblems({ access: fieldError(error, 'access') })
      },
    })
  }

  const select = (control: { className: string }) => control.className.replace('form-control', 'form-select')

  return (
    <div className="card mb-0">
      <div className="card-header">
        <i className="fas fa-user-shield text-muted" aria-hidden="true" />
        <h2 className="h6 mb-0">Beri atau ubah akses</h2>
      </div>
      <div className="card-body">
        {empty && (
          <div className="mb-3">
            <Notice kind="warning">{groups.length === 0 ? 'Buat group terlebih dahulu sebelum memberi akses.' : 'Tambahkan koneksi database terlebih dahulu sebelum memberi akses.'}</Notice>
          </div>
        )}
        {general && (
          <div className="mb-3">
            <Notice kind="error">{general}</Notice>
          </div>
        )}
        <form onSubmit={submit} noValidate aria-busy={mutation.isPending}>
          <Field id="grant-group" label="Group" error={problems.group}>
            {(control) => (
              <select
                {...control}
                className={select(control)}
                name="group_id"
                ref={groupRef}
                value={groupId}
                onChange={(event) => {
                  setGroupId(event.target.value)
                  clear('group')
                }}
              >
                <option value="">Pilih group</option>
                {groups.map((group) => (
                  <option key={group.id} value={group.id}>
                    {group.name}
                  </option>
                ))}
              </select>
            )}
          </Field>
          <Field id="grant-database" label="Database" error={problems.database}>
            {(control) => (
              <select
                {...control}
                className={select(control)}
                name="database_id"
                ref={databaseRef}
                value={databaseId}
                onChange={(event) => {
                  setDatabaseId(event.target.value)
                  clear('database')
                }}
              >
                <option value="">Pilih database</option>
                {connections.map((connection) => (
                  <option key={connection.id} value={connection.id}>
                    {connection.name}
                  </option>
                ))}
              </select>
            )}
          </Field>
          <Field id="grant-access" label="Level akses" error={problems.access}>
            {(control) => (
              <select {...control} className={select(control)} name="access" ref={accessRef} value={access} onChange={(event) => setAccess(event.target.value as Access)}>
                <option value="view_only">Lihat saja</option>
                <option value="view_edit">Lihat + ubah (migration)</option>
              </select>
            )}
          </Field>
          <button type="submit" className="btn btn-primary w-100" disabled={mutation.isPending || empty}>
            {mutation.isPending ? (
              <>
                <span className="spinner-border spinner-border-sm me-2" aria-hidden="true" />
                Menyimpan
              </>
            ) : (
              <>
                <i className="fas fa-floppy-disk" aria-hidden="true" />
                Simpan akses
              </>
            )}
          </button>
        </form>
      </div>
    </div>
  )
}
