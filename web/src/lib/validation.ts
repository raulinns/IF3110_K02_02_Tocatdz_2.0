const usernamePattern = /^[a-z0-9_]{3,32}$/
const maxNameLength = 64
const maxPasswordBytes = 1024
const maxDsnBytes = 4096

const byteLength = (text: string) => new TextEncoder().encode(text).length
const length = (text: string) => Array.from(text).length

// Aturan ini hanya petunjuk cepat di form dan backend tetap yang menentukan
export function normalizeUsername(raw: string): string {
  return raw.trim().toLowerCase()
}

export function usernameProblem(username: string): string | null {
  if (username === '') return 'Username wajib diisi'
  return usernamePattern.test(username) ? null : 'Username harus 3-32 karakter berupa huruf kecil, angka, atau garis bawah'
}

// Panjang password dihitung dalam karakter Unicode, sedangkan batas atasnya dalam byte
export function passwordProblem(password: string): string | null {
  if (password === '') return 'Password wajib diisi'
  if (length(password) < 15) return 'Password minimal 15 karakter'
  if (byteLength(password) > maxPasswordBytes) return 'Password maksimal 1024 byte'
  return null
}

export function nameProblem(name: string, label: string): string | null {
  if (name === '') return `${label} wajib diisi`
  return length(name) > maxNameLength ? `${label} maksimal ${maxNameLength} karakter` : null
}

export function dsnProblem(dsn: string): string | null {
  if (dsn === '') return 'Connection string wajib diisi'
  return byteLength(dsn) > maxDsnBytes ? 'Connection string terlalu panjang' : null
}
