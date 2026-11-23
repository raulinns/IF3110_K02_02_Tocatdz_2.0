import { describe, expect, it } from 'vitest'
import { defaultLanding, safeRedirectPath } from './redirect.ts'

describe('safeRedirectPath', () => {
  it.each(['/databases/abc/history', '/databases/abc/history?hide_rollback=true&page=2', '/admin/users#bagian', '/', '/loginx', '/databases/login'])('menerima %s', (path) => {
    expect(safeRedirectPath(path)).toBe(path)
  })

  it.each([undefined, null, 5, {}, '', 'databases', 'https://evil.example/x', '//evil.example', '///evil.example', '/\\evil.example', '/a\\b', '/a\nb', '/a\u0000b', `/${'a'.repeat(2048)}`])('menolak %j', (value) => {
    expect(safeRedirectPath(value)).toBe(defaultLanding)
  })

  it.each(['/login', '/login?x=1', '/login/', '/login#a'])('tidak mengembalikan pengguna ke halaman login: %s', (path) => {
    expect(safeRedirectPath(path)).toBe(defaultLanding)
  })
})
