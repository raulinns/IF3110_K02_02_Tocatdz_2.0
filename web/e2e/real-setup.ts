import { mkdirSync } from 'node:fs'
import { request } from '@playwright/test'
import { passwords, roles, statePath } from './real-support'

// Setiap peran masuk satu kali lewat API lalu sesinya dipakai ulang oleh semua spec dan project, karena server membatasi login per akun
export default async function setup() {
  const base = process.env.E2E_BASE_URL
  if (!base) return
  mkdirSync('e2e/.auth', { recursive: true })
  for (const role of roles) {
    if (!passwords[role]) throw new Error(`kata sandi ${role} belum diisi, setel E2E_ADMIN_PASSWORD, E2E_VIEWER_PASSWORD, dan E2E_EDITOR_PASSWORD`)
    const context = await request.newContext({ baseURL: base, extraHTTPHeaders: { Origin: new URL(base).origin } })
    const response = await context.post('/api/v1/auth/login', { data: { username: role, password: passwords[role] } })
    if (!response.ok()) throw new Error(`login ${role} gagal dengan status ${response.status()}`)
    await context.storageState({ path: statePath(role) })
    await context.dispose()
  }
}
