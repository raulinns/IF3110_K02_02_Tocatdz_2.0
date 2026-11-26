import { test, type Browser, type Page } from '@playwright/test'

// Akun seed server sungguhan dan kata sandinya dari lingkungan: E2E_ADMIN_PASSWORD, E2E_VIEWER_PASSWORD, E2E_EDITOR_PASSWORD
export const roles = ['admin', 'pengamat', 'editor'] as const
export type Role = (typeof roles)[number]

export const passwords: Record<Role, string> = {
  admin: process.env.E2E_ADMIN_PASSWORD ?? '',
  pengamat: process.env.E2E_VIEWER_PASSWORD ?? '',
  editor: process.env.E2E_EDITOR_PASSWORD ?? '',
}

// Sesi tiap peran disimpan sekali oleh real-setup karena server membatasi login per akun
export const statePath = (role: Role) => `e2e/.auth/${role}.json`

// Kesalahan halaman, pelanggaran CSP, dan respons gagal dikumpulkan karena CSP server lebih ketat daripada dev server
export function watch(page: Page) {
  const problems: string[] = []
  page.on('pageerror', (error) => problems.push(`pageerror: ${error.message}`))
  page.on('console', (message) => {
    // Baris "Failed to load resource" tidak memuat URL sehingga respons gagalnya dicatat terpisah di bawah
    if (message.type() === 'error' && !message.text().startsWith('Failed to load resource')) problems.push(`console: ${message.text()}`)
  })
  page.on('response', (response) => {
    const url = new URL(response.url())
    // Cek sesi saat belum masuk memang menghasilkan 401
    const expected = response.status() === 401 && url.pathname === '/api/v1/auth/me'
    // Server membalas 409 pada pembacaan paralel yang memakai satu sesi (rebutan baris sesi), dan SPA mengulang pembacaan itu satu kali
    // Respons ini dicatat di laporan sebagai anotasi cacat backend dan bukan disembunyikan, sedangkan galat lain tetap menggagalkan test
    if (response.status() === 409 && response.request().method() === 'GET') {
      test.info().annotations.push({ type: 'cacat-backend', description: `409 pada GET ${url.pathname}` })
      return
    }
    if (response.status() >= 400 && !expected) problems.push(`${response.status()} ${url.pathname}`)
  })
  return problems
}

// Konteks baru dengan sesi peran itu dan emulasi perangkat yang sama dengan project yang sedang berjalan
export async function openAs(browser: Browser, role: Role) {
  const { baseURL, viewport, isMobile, hasTouch, userAgent, deviceScaleFactor } = test.info().project.use
  const context = await browser.newContext({ baseURL, viewport: viewport ?? undefined, isMobile, hasTouch, userAgent, deviceScaleFactor, storageState: statePath(role) })
  const page = await context.newPage()
  return { page, context, problems: watch(page) }
}
