import { useState, type ChangeEventHandler, type Ref } from 'react'
import type { ControlProps } from './Field.tsx'

interface PasswordInputProps extends ControlProps {
  name?: string
  value: string
  onChange: ChangeEventHandler<HTMLInputElement>
  autoComplete: 'current-password' | 'new-password'
  placeholder?: string
  error?: string
  inputRef?: Ref<HTMLInputElement>
}

// Input password dengan tombol tampilkan atau sembunyikan, dan pesan error berada di dalam input group agar muncul di bawahnya
export function PasswordInput({ error, inputRef, value, onChange, name, autoComplete, placeholder, id, className, ...aria }: PasswordInputProps) {
  const [revealed, setRevealed] = useState(false)
  return (
    <div className="input-group has-validation">
      <input id={id} name={name ?? id} type={revealed ? 'text' : 'password'} ref={inputRef} className={className} value={value} onChange={onChange} placeholder={placeholder} autoComplete={autoComplete} {...aria} />
      <button type="button" className="btn btn-outline-secondary" aria-pressed={revealed} aria-label={revealed ? 'Sembunyikan password' : 'Tampilkan password'} onClick={() => setRevealed((current) => !current)}>
        <i className={`fas ${revealed ? 'fa-eye-slash' : 'fa-eye'}`} aria-hidden="true" />
      </button>
      {error && (
        <div id={`${id}-error`} className="invalid-feedback">
          {error}
        </div>
      )}
    </div>
  )
}
