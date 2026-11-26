import { expect, test, type Locator, type Page } from '@playwright/test'

// Kata sandi demo dari data seed mock
export const password = 'tocatdz-demo-2026'

export type Account = 'reviewer' | 'observer' | 'editor' | 'admin' | 'auditor' | 'pengamat' | 'nonaktif'

// State mock hidup di memori halaman sehingga alur dengan beberapa akun harus berpindah lewat form login tanpa memuat ulang halaman
export async function login(page: Page, username: Account, pass = password) {
  await page.getByLabel('Username').fill(username)
  await page.getByLabel('Password', { exact: true }).fill(pass)
  await page.getByRole('button', { name: 'Masuk' }).click()
}

export async function logout(page: Page) {
  await page.getByRole('button', { name: 'Keluar' }).click()
  await expect(page.getByRole('heading', { name: 'Masuk ke akun Anda' })).toBeVisible()
}

export async function startAs(page: Page, username: Account) {
  await page.goto('/login')
  await login(page, username)
  await expect(page).not.toHaveURL(/\/login/)
}

export async function switchTo(page: Page, username: Account) {
  await logout(page)
  await login(page, username)
  await expect(page).not.toHaveURL(/\/login/)
}

// Daftar tab database ada di dalam halaman sehingga cara membukanya sama di desktop dan mobile
export async function openTab(page: Page, name: string) {
  await page.getByRole('list', { name: 'Bagian database' }).getByRole('link', { name, exact: true }).click()
}

// Masuk ke sebuah database lewat klik saja karena memuat ulang halaman akan menghapus state mock
export async function enterDatabase(page: Page, name = 'inventaris') {
  if (!page.url().endsWith('/databases')) await page.getByRole('navigation', { name: 'Breadcrumb' }).getByRole('link', { name: 'Database', exact: true }).first().click()
  await page.getByRole('row').filter({ hasText: name }).getByRole('link', { name: 'Schema' }).click()
  await expect(page.getByRole('list', { name: 'Bagian database' })).toBeVisible()
}

// Editor SQL berupa CodeMirror sehingga diisi lewat papan ketik sungguhan, dan di profil mobile lewat commit teks seperti papan ketik layar
export async function typeSql(page: Page, name: string, text: string) {
  const editor = page.getByRole('textbox', { name, exact: true })
  await editor.click()
  await page.keyboard.press('Control+A')
  if (test.info().project.use.isMobile) await page.keyboard.insertText(text)
  else await page.keyboard.type(text)
  await expect(editor).toContainText(text.split('\n')[0].slice(0, 20))
}

// Dibandingkan dengan viewport visual karena Chrome mobile melebarkan viewport layout bila ada konten yang meluber
export async function expectNoHorizontalScroll(page: Page) {
  const overflow = await page.evaluate(() => document.documentElement.scrollWidth - (window.visualViewport?.width ?? window.innerWidth))
  expect(overflow, 'halaman tidak boleh punya scroll horizontal').toBeLessThanOrEqual(0)
}

export const rowWith = (page: Page, ...texts: string[]): Locator => texts.reduce((rows, text) => rows.filter({ hasText: text }), page.getByRole('row'))
