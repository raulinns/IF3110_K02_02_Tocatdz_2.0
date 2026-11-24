import type { ReactNode } from 'react'

export interface ControlProps {
  id: string
  className: string
  'aria-invalid'?: true
  'aria-describedby'?: string
}

interface FieldProps {
  id: string
  label: string
  error?: string
  hint?: ReactNode
  className?: string
  // Kontrol yang menampilkan pesan errornya sendiri, misalnya input group, mematikan pesan bawaan di sini
  feedback?: boolean
  children: (control: ControlProps) => ReactNode
}

// Label, kontrol, pesan error, dan petunjuk dihubungkan lewat id supaya terbaca benar oleh pembaca layar
export function Field({ id, label, error, hint, className = 'mb-3', feedback = true, children }: FieldProps) {
  const describedBy = [error ? `${id}-error` : null, hint ? `${id}-hint` : null].filter(Boolean).join(' ')
  return (
    <div className={`${className} text-start`}>
      <label htmlFor={id} className="form-label">
        {label}
      </label>
      {children({ id, className: `form-control${error ? ' is-invalid' : ''}`, 'aria-invalid': error ? true : undefined, 'aria-describedby': describedBy || undefined })}
      {feedback && error && (
        <div id={`${id}-error`} className="invalid-feedback">
          {error}
        </div>
      )}
      {hint && (
        <div id={`${id}-hint`} className="form-text">
          {hint}
        </div>
      )}
    </div>
  )
}
