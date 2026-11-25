import { lazy, Suspense, useId, type ReactNode } from 'react'

// CodeMirror cukup besar sehingga baru dimuat saat halaman yang memakai editor dibuka
const SqlEditor = lazy(() => import('./SqlEditor.tsx'))

interface SqlFieldProps {
  label: string
  value: string
  onChange?: (value: string) => void
  readOnly?: boolean
  errors?: string[]
  hint?: ReactNode
  placeholder?: string
  // Tinggi minimum editor dalam rem
  rows?: number
  className?: string
}

// Label, editor, petunjuk, dan pesan error terhubung lewat id supaya terbaca benar oleh pembaca layar
export function SqlField({ label, value, onChange, readOnly = false, errors = [], hint, placeholder, rows = 8, className = 'mb-3' }: SqlFieldProps) {
  const id = useId()
  const labelId = `${id}-label`
  const errorId = `${id}-error`
  const hintId = `${id}-hint`
  const describedBy = [errors.length > 0 ? errorId : null, hint ? hintId : null].filter(Boolean).join(' ')
  return (
    <div className={`${className} sql-field${errors.length > 0 ? ' is-invalid' : ''}`}>
      <div id={labelId} className="form-label">
        {label}
      </div>
      <Suspense fallback={<div className="sql-editor-loading" role="status" style={{ minHeight: `${rows * 1.6 + 1}rem` }}>Memuat editor…</div>}>
        <div style={{ ['--editor-min' as string]: `${rows * 1.6}rem` }}>
          <SqlEditor value={value} onChange={onChange} readOnly={readOnly} labelledBy={labelId} describedBy={describedBy || undefined} invalid={errors.length > 0} placeholder={placeholder} />
        </div>
      </Suspense>
      {errors.length > 0 && (
        <ul id={errorId} className="sql-errors">
          {errors.map((error) => (
            <li key={error}>{error}</li>
          ))}
        </ul>
      )}
      {hint && (
        <div id={hintId} className="form-text">
          {hint}
        </div>
      )}
    </div>
  )
}
