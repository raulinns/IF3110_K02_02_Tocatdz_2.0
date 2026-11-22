import { ApiFailure, utf8Length } from './http.ts'

const usernamePattern = /^[a-z0-9_]{3,32}$/
const maxNameRunes = 64
const maxDsnBytes = 4096

// Username dinormalkan dengan trim dan huruf kecil seperti di backend
export function normalizeUsername(raw: string): string {
  return raw.trim().toLowerCase()
}

export function usernameError(username: string): string | null {
  return usernamePattern.test(username) ? null : 'Username harus 3-32 karakter berupa huruf kecil, angka, atau garis bawah'
}

export function passwordError(password: string): string | null {
  if (Array.from(password).length < 15) return 'Password minimal 15 karakter'
  if (utf8Length(password) > 1024) return 'Password maksimal 1024 byte'
  return null
}

export function nameError(name: string, label: string): string | null {
  if (name === '') return `${label} wajib diisi`
  if (Array.from(name).length > maxNameRunes) return `${label} maksimal ${maxNameRunes} karakter`
  return null
}

export function dsnError(dsn: string, engine: string | undefined): string | null {
  if (dsn === '') return 'DSN wajib diisi'
  if (utf8Length(dsn) > maxDsnBytes) return 'DSN terlalu panjang'
  if (engine === 'postgresql' && !/^postgres(ql)?:\/\//i.test(dsn)) return 'DSN PostgreSQL harus berbentuk URL postgres://'
  return null
}

// Mengumpulkan pesan per field lalu melempar satu error validasi, kunci field sama dengan path di request
export class Fields {
  private readonly fields: Record<string, string[]> = {}

  add(field: string, message: string | null) {
    if (message) (this.fields[field] ??= []).push(message)
  }

  check() {
    if (Object.keys(this.fields).length > 0) throw new ApiFailure(422, 'validation_failed', 'Input tidak valid', { fields: this.fields })
  }
}
