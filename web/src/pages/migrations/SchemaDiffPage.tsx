import { useRef, useState, type FormEvent } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate, useSearchParams } from 'react-router-dom'
import { databasesQuery } from '../../api/explorer.ts'
import { schemaDiffQuery } from '../../api/migrations.ts'
import { errorMessage, fieldError } from '../../api/messages.ts'
import type { Schemas } from '../../api/types.ts'
import { ApiErrorView } from '../../components/ui/ApiErrorView.tsx'
import { Field } from '../../components/ui/Field.tsx'
import { Notice } from '../../components/ui/Notice.tsx'
import { BoxSkeleton } from '../../components/ui/Skeleton.tsx'
import { SqlField } from '../../components/ui/SqlField.tsx'
import { emptyForm } from '../../lib/definition.ts'
import { newMigrationPath } from '../../lib/paths.ts'
import { useDatabase } from '../../lib/useDatabase.ts'

const actionLabel: Record<Schemas['SchemaChange']['action'], string> = { add: 'Tambah', alter: 'Ubah', drop: 'Hapus' }
const maxIdentifier = 63

interface Draft {
  desired: string
  currentSchema: string
  desiredSchema: string
}

type Problems = Partial<Record<keyof Draft, string>>

function schemaProblem(value: string, label: string): string | undefined {
  if (value.trim() === '') return `${label} wajib diisi`
  return new TextEncoder().encode(value).length > maxIdentifier ? `${label} maksimal ${maxIdentifier} byte` : undefined
}

export function SchemaDiffPage() {
  const { db, canEdit } = useDatabase()
  const navigate = useNavigate()
  const [params, setParams] = useSearchParams()
  const databases = useQuery(databasesQuery())
  const desired = params.get('desired') ?? ''
  const applied: Draft = { desired, currentSchema: params.get('current_schema') ?? 'public', desiredSchema: params.get('desired_schema') ?? 'public' }
  const [draft, setDraft] = useState<Draft>(applied)
  const [problems, setProblems] = useState<Problems>({})
  const refs = { desired: useRef<HTMLSelectElement>(null), currentSchema: useRef<HTMLInputElement>(null), desiredSchema: useRef<HTMLInputElement>(null) }

  const comparable = databases.data?.filter((database) => database.capabilities.schema_diff) ?? []
  const query = useQuery({ ...schemaDiffQuery(db, applied), enabled: desired !== '' })
  const desiredName = databases.data?.find((database) => database.id === desired)?.name ?? ''
  const diff = query.data
  const serverProblems: Problems = { desired: undefined, currentSchema: fieldError(query.error, 'current_schema'), desiredSchema: fieldError(query.error, 'desired_schema') }
  const fieldFailed = Boolean(serverProblems.currentSchema || serverProblems.desiredSchema)

  function edit(patch: Partial<Draft>) {
    setDraft((current) => ({ ...current, ...patch }))
    setProblems({})
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    const found: Problems = {
      desired: draft.desired === '' ? 'Pilih database pembanding' : undefined,
      currentSchema: schemaProblem(draft.currentSchema, 'Schema saat ini'),
      desiredSchema: schemaProblem(draft.desiredSchema, 'Schema pembanding'),
    }
    setProblems(found)
    // Urutan sama dengan urutan isian di layar supaya fokus pindah ke isian bermasalah yang paling atas
    const first = (['currentSchema', 'desired', 'desiredSchema'] as const).find((key) => found[key])
    if (first) {
      refs[first].current?.focus()
      return
    }
    setParams({ desired: draft.desired, current_schema: draft.currentSchema.trim(), desired_schema: draft.desiredSchema.trim() })
  }

  const createDraft = (generated: string) =>
    void navigate(newMigrationPath(db), {
      state: { prefill: { form: { ...emptyForm(), title: `Samakan schema ${applied.currentSchema} dengan ${desiredName}.${applied.desiredSchema}`, upSql: generated }, source: 'diff' } },
    })

  const message = (key: keyof Draft) => problems[key] ?? serverProblems[key]
  const count = (action: Schemas['SchemaChange']['action']) => diff?.changes.filter((change) => change.action === action).length ?? 0

  return (
    <>
      <div className="card">
        <div className="card-header">
          <i className="fas fa-code-compare text-muted" aria-hidden="true" />
          <h2 className="h6 mb-0">Bandingkan schema</h2>
        </div>
        <div className="card-body">
          <p className="text-muted small">Pilih kondisi schema yang diinginkan sebagai pembanding. SQL kandidat mengubah schema database ini agar sama dengan pembanding dan tidak dijalankan sampai Anda menyimpannya sebagai draft lalu mengajukannya.</p>
          <form onSubmit={submit} noValidate>
            <div className="row g-3 align-items-start">
              <div className="col-md-3">
                <Field id="diff-current-schema" label="Schema saat ini" error={message('currentSchema')} className="mb-0">
                  {(control) => <input {...control} className={`${control.className} mono`} name="current_schema" type="text" ref={refs.currentSchema} value={draft.currentSchema} onChange={(event) => edit({ currentSchema: event.target.value })} autoComplete="off" spellCheck={false} />}
                </Field>
              </div>
              <div className="col-md-4">
                <Field id="diff-desired" label="Database pembanding" error={message('desired')} className="mb-0">
                  {(control) => (
                    <select {...control} className={control.className.replace('form-control', 'form-select')} name="desired" ref={refs.desired} value={draft.desired} onChange={(event) => edit({ desired: event.target.value })}>
                      <option value="">Pilih database</option>
                      {comparable.map((database) => (
                        <option key={database.id} value={database.id}>
                          {database.name}
                          {database.id === db ? ' (database ini)' : ''}
                        </option>
                      ))}
                    </select>
                  )}
                </Field>
              </div>
              <div className="col-md-3">
                <Field id="diff-desired-schema" label="Schema pembanding" error={message('desiredSchema')} className="mb-0">
                  {(control) => <input {...control} className={`${control.className} mono`} name="desired_schema" type="text" ref={refs.desiredSchema} value={draft.desiredSchema} onChange={(event) => edit({ desiredSchema: event.target.value })} autoComplete="off" spellCheck={false} />}
                </Field>
              </div>
              <div className="col-md-2">
                <button type="submit" className="btn btn-primary w-100" disabled={query.isFetching}>
                  <i className="fas fa-magnifying-glass" aria-hidden="true" />
                  Bandingkan
                </button>
              </div>
            </div>
          </form>
        </div>
      </div>

      {desired !== '' && query.isFetching && <BoxSkeleton rows={4} />}
      {query.isError && !query.isFetching && (fieldFailed ? <Notice kind="error">{errorMessage(query.error)}</Notice> : <ApiErrorView error={query.error} onRetry={() => void query.refetch()} />)}

      {diff && !query.isFetching && (
        <>
          <div className="row">
            {[
              { label: 'Perubahan', value: diff.changes.length, icon: 'fa-list-check', note: `${applied.currentSchema} → ${desiredName}.${applied.desiredSchema}` },
              { label: 'Tambah', value: count('add'), icon: 'fa-plus', note: 'Objek baru' },
              { label: 'Ubah', value: count('alter'), icon: 'fa-pen', note: 'Objek yang berbeda' },
              { label: 'Hapus', value: count('drop'), icon: 'fa-trash', note: `${diff.destructive_count} berisiko menghilangkan data` },
            ].map((stat) => (
              <div className="col-6 col-xl-3" key={stat.label}>
                <div className="card">
                  <div className="content">
                    <div className="row">
                      <div className="col-4">
                        <div className="icon-big text-center">
                          <i className={`fas ${stat.icon}`} aria-hidden="true" />
                        </div>
                      </div>
                      <div className="col-8">
                        <div className="detail">
                          <p className="detail-subtitle">{stat.label}</p>
                          <span className="number">{stat.value}</span>
                        </div>
                      </div>
                    </div>
                    <div className="footer">
                      <hr className="my-2" />
                      <div className="stats">
                        <i className="fas fa-circle-info" aria-hidden="true" /> {stat.note}
                      </div>
                    </div>
                  </div>
                </div>
              </div>
            ))}
          </div>

          {diff.changes.length > 0 ? (
            <div className="card">
              <div className="card-header">
                <i className="fas fa-list-check text-muted" aria-hidden="true" />
                <h2 className="h6 mb-0">Perbedaan</h2>
              </div>
              <div className="table-responsive" tabIndex={0} role="region" aria-label="Daftar perbedaan schema">
                <table className="table table-hover" aria-label="Perbedaan schema">
                  <thead>
                    <tr>
                      <th scope="col">Aksi</th>
                      <th scope="col">Objek</th>
                      <th scope="col">Nama</th>
                      <th scope="col">Saat ini</th>
                      <th scope="col">Diinginkan</th>
                    </tr>
                  </thead>
                  <tbody>
                    {diff.changes.map((change, index) => (
                      <tr key={index}>
                        <td className="text-nowrap">
                          <span className={`badge diff-${change.action}`}>{actionLabel[change.action]}</span>
                          {change.destructive && <i className="fas fa-triangle-exclamation text-danger-soft ms-1" role="img" aria-label="Data bisa hilang" title="Data bisa hilang" />}
                        </td>
                        <td>{change.object}</td>
                        <td className="mono">{change.name}</td>
                        <td className="mono diff-cell">{change.before || '-'}</td>
                        <td className="mono diff-cell">{change.after || '-'}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </div>
          ) : (
            <div className="box box-primary">
              <div className="empty-state">
                <i className="fas fa-circle-check" aria-hidden="true" />
                <p className="mb-0">
                  Tidak ada perbedaan. Schema <span className="mono">{applied.currentSchema}</span> sudah sama dengan <span className="mono">{desiredName}.{applied.desiredSchema}</span>.
                </p>
              </div>
            </div>
          )}

          {diff.warnings.length > 0 && (
            <Notice kind="warning">
              <strong>Perlu ditinjau</strong>
              <ul className="mb-0 ps-3">
                {diff.warnings.map((warning) => (
                  <li key={warning}>{warning}</li>
                ))}
              </ul>
            </Notice>
          )}

          {diff.generated_up_sql !== '' && (
            <div className="card">
              <div className="card-header">
                <i className="fas fa-code text-muted" aria-hidden="true" />
                <h2 className="h6 mb-0">SQL kandidat</h2>
                <span className="card-header-meta">Hanya baca</span>
              </div>
              <div className="card-body">
                {diff.validation !== '' ? <Notice kind="error">SQL kandidat belum lolos validasi migration: {diff.validation} Ubah SQL di editor setelah membuat draft.</Notice> : <Notice kind="success">SQL kandidat lolos validasi migration. Tetap tinjau setiap perintah sebelum disimpan sebagai draft.</Notice>}
                <SqlField label="SQL up kandidat" value={diff.generated_up_sql} readOnly rows={6} className="mb-3" />
                {canEdit ? (
                  <>
                    <button type="button" className="btn btn-primary" onClick={() => createDraft(diff.generated_up_sql)}>
                      <i className="fas fa-file-circle-plus" aria-hidden="true" />
                      Buat draft migration dari diff
                    </button>
                    <div className="form-text">SQL down tidak dibuat otomatis. Anda perlu mengisinya atau menandai migration sebagai irreversible di editor.</div>
                  </>
                ) : (
                  <p className="text-muted small mb-0">Anda tidak punya akses membuat migration pada database ini.</p>
                )}
              </div>
            </div>
          )}
        </>
      )}
    </>
  )
}
