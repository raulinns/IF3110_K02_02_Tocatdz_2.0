import { useEffect, useRef, useState, type FormEvent, type ReactNode } from 'react'
import { Link } from 'react-router-dom'
import type { Definition } from '../../api/migrations.ts'
import { isApiError } from '../../api/errors.ts'
import { errorMessage } from '../../api/messages.ts'
import { Notice } from '../../components/ui/Notice.tsx'
import { definitionProblems, fieldOrder, firstProblem, sameForm, toDefinition, type DefinitionForm, type Problems } from '../../lib/definition.ts'
import { MigrationForm } from './MigrationForm.tsx'

interface DefinitionEditorProps {
  initial: DefinitionForm
  readOnly: boolean
  submitLabel: string
  onSave: (definition: Definition) => Promise<void>
  // Dipanggil saat server menolak karena revision berubah atau definisi sudah dibekukan
  onReload?: () => void
  onDirtyChange?: (dirty: boolean) => void
  // Tombol tambahan di samping tombol simpan, misalnya mengajukan review
  extraActions?: ReactNode
  cancelTo: string
}

const staleReasons = new Set(['revision_mismatch', 'definition_frozen'])

export function DefinitionEditor({ initial, readOnly, submitLabel, onSave, onReload, onDirtyChange, extraActions, cancelTo }: DefinitionEditorProps) {
  const form = useRef<HTMLFormElement>(null)
  const inFlight = useRef(false)
  const [value, setValue] = useState(initial)
  const [problems, setProblems] = useState<Problems>({})
  const [failure, setFailure] = useState<unknown>(null)
  const [saving, setSaving] = useState(false)
  const dirty = !sameForm(value, initial)

  useEffect(() => {
    onDirtyChange?.(dirty)
  }, [dirty, onDirtyChange])

  // Peramban menanyakan konfirmasi bila tab ditutup saat ada perubahan yang belum disimpan
  useEffect(() => {
    if (!dirty) return
    const warn = (event: BeforeUnloadEvent) => event.preventDefault()
    window.addEventListener('beforeunload', warn)
    return () => window.removeEventListener('beforeunload', warn)
  }, [dirty])

  function focusProblem(found: Problems) {
    const field = firstProblem(found)
    if (!field) return
    form.current?.querySelector<HTMLElement>(`[data-problem-field="${field}"], [data-field="${field}"] .cm-content`)?.focus()
  }

  function edited(...fields: string[]) {
    setProblems((current) => {
      if (!fields.some((field) => current[field])) return current
      const next = { ...current }
      for (const field of fields) delete next[field]
      return next
    })
    setFailure(null)
  }

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault()
    // Ref dipakai karena status menyimpan baru terlihat setelah render ulang dan klik cepat bisa lolos
    if (readOnly || inFlight.current) return
    const found = definitionProblems(value)
    setFailure(null)
    setProblems(found)
    if (firstProblem(found)) {
      focusProblem(found)
      return
    }
    inFlight.current = true
    setSaving(true)
    try {
      await onSave(toDefinition(value))
    } catch (error) {
      const fields = isApiError(error) ? error.fields : {}
      setProblems(fields)
      setFailure(error)
      focusProblem(fields)
    } finally {
      inFlight.current = false
      setSaving(false)
    }
  }

  const visible = Object.keys(problems).some((field) => fieldOrder.includes(field))
  const stale = isApiError(failure) && failure.reason !== undefined && staleReasons.has(failure.reason)

  return (
    <form ref={form} onSubmit={(event) => void submit(event)} noValidate aria-busy={saving}>
      {failure !== null && !visible && (
        <Notice kind="error">
          <div>{errorMessage(failure)}</div>
          {stale && onReload && (
            <button type="button" className="btn btn-sm btn-outline-secondary mt-2" onClick={onReload}>
              <i className="fas fa-rotate-right" aria-hidden="true" />
              Muat ulang dari server
            </button>
          )}
        </Notice>
      )}
      <MigrationForm value={value} onChange={setValue} problems={problems} readOnly={readOnly} onEdited={edited} />
      {!readOnly && (
        <div className="d-flex flex-wrap align-items-center gap-2 mt-4">
          <button type="submit" className="btn btn-primary" disabled={saving}>
            {saving ? (
              <>
                <span className="spinner-border spinner-border-sm me-2" aria-hidden="true" />
                Menyimpan
              </>
            ) : (
              <>
                <i className="fas fa-floppy-disk" aria-hidden="true" />
                {submitLabel}
              </>
            )}
          </button>
          {extraActions}
          <Link className="btn btn-light" to={cancelTo}>
            Kembali
          </Link>
          {dirty && <span className="text-muted small">Ada perubahan yang belum disimpan</span>}
        </div>
      )}
    </form>
  )
}
