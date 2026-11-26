import { expect, test, type Browser, type Page } from '@playwright/test'
import { login, rowWith } from './support'

// Spec ini hanya berjalan terhadap server Go sungguhan: E2E_BASE_URL=http://localhost:8080 E2E_ADMIN_PASSWORD=... E2E_VIEWER_PASSWORD=... E2E_EDITOR_PASSWORD=... npx playwright test --project=desktop
const passwords = { admin: process.env.E2E_ADMIN_PASSWORD ?? '', pengamat: process.env.E2E_VIEWER_PASSWORD ?? '', editor: process.env.E2E_EDITOR_PASSWORD ?? '' }
type Role = keyof typeof passwords

test.skip(!process.env.E2E_BASE_URL, 'khusus server sungguhan')
test.describe.configure({ mode: 'serial' })

// State server tersimpan antar test sehingga nama dibuat unik per run
const group = `uji-${Date.now().toString(36)}`
const sessions: Partial<Record<Role, Awaited<ReturnType<ReturnType<Page['context']>['storageState']>>>> = {}

// Server membatasi laju login sehingga setiap peran masuk satu kali lalu sesinya dipakai ulang di semua test
test.beforeAll(async ({ browser }) => {
  for (const role of Object.keys(passwords) as Role[]) {
    const context = await browser.newContext({ baseURL: test.info().project.use.baseURL })
    const page = await context.newPage()
    await page.goto('/login')
    await login(page, role, passwords[role])
    await expect(page).not.toHaveURL(/\/login/)
    sessions[role] = await context.storageState()
    await context.close()
  }
})

// Kesalahan halaman, pelanggaran CSP, dan respons gagal dikumpulkan karena CSP server lebih ketat daripada dev server
function watch(page: Page) {
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
    if (response.status() >= 400 && !expected) problems.push(`${response.status()} ${url.pathname}`)
  })
  return problems
}

async function open(browser: Browser, role: Role) {
  const info = test.info().project.use
  const context = await browser.newContext({ baseURL: info.baseURL, viewport: info.viewport ?? undefined, storageState: sessions[role] })
  const page = await context.newPage()
  return { page, context, problems: watch(page) }
}

test('halaman dimuat tanpa kesalahan konsol atau pelanggaran CSP', async ({ browser }) => {
  const { page, context, problems } = await open(browser, 'admin')
  await page.goto('/admin/users')
  await expect(page.getByRole('table', { name: 'Daftar user' })).toBeVisible()
  await page.goto('/admin/groups')
  await expect(page.getByRole('table', { name: 'Daftar group' })).toBeVisible()
  expect(problems).toEqual([])
  await context.close()
})

test('admin membuat group, menambah anggota, menetapkan leader, dan memberi akses ke database demo', async ({ browser }) => {
  const { page, context } = await open(browser, 'admin')
  await page.goto('/admin/groups')
  await page.getByLabel('Nama group').fill(group)
  await page.getByRole('button', { name: 'Buat group' }).click()
  await expect(page.getByText(`Group ${group} berhasil dibuat.`)).toBeVisible()
  await rowWith(page, group).getByRole('link', { name: `Kelola group ${group}` }).click()
  await page.getByLabel('Username').fill('pengamat')
  await page.getByRole('button', { name: 'Tambahkan' }).click()
  await expect(page.getByText(`pengamat ditambahkan ke group ${group}.`)).toBeVisible()
  await rowWith(page, 'pengamat').getByRole('button', { name: 'Jadikan leader' }).click()
  await page.getByRole('dialog', { name: 'Ganti leader' }).getByRole('button', { name: 'Jadikan leader' }).click()
  await expect(page.getByText(`pengamat sekarang menjadi leader group ${group}.`)).toBeVisible()
  for (const database of ['demo', 'gudang']) {
    await page.goto('/admin/grants')
    await page.getByLabel('Group', { exact: true }).selectOption({ label: group })
    await page.getByLabel('Database', { exact: true }).selectOption({ label: database })
    await page.getByRole('button', { name: 'Simpan akses' }).click()
    await expect(page.getByText(new RegExp(`ke database ${database} disimpan: Lihat saja\\.`))).toBeVisible()
  }
  await context.close()
})

test('nama group yang sudah dipakai ditolak dengan pesan yang jelas di bawah isiannya', async ({ browser }) => {
  const { page, context } = await open(browser, 'admin')
  await page.goto('/admin/groups')
  await page.getByLabel('Nama group').fill(group)
  await page.getByRole('button', { name: 'Buat group' }).click()
  await expect(page.getByText('Nama group sudah dipakai')).toBeVisible()
  await context.close()
})

test('pengamat menjelajah schema, tabel, dan data baris database demo', async ({ browser }) => {
  const { page, context, problems } = await open(browser, 'pengamat')
  await page.goto('/databases')
  const demo = rowWith(page, 'demo')
  await expect(demo.getByText('Lihat saja')).toBeVisible()
  await demo.getByRole('link', { name: 'Schema' }).click()
  await page.getByRole('link', { name: 'pelanggan' }).click()
  await expect(page.getByRole('heading', { name: 'Kolom' })).toBeVisible()
  await expect(page.getByRole('cell', { name: 'email', exact: true }).first()).toBeVisible()
  await page.getByRole('link', { name: 'Lihat data' }).click()
  await expect(page.getByRole('table', { name: /Isi tabel public.pelanggan/ })).toContainText('Budi')
  expect(problems).toEqual([])
  await context.close()
})

test('schema diff antar dua database nyata menampilkan hasil dan SQL kandidat', async ({ browser }) => {
  const { page, context } = await open(browser, 'pengamat')
  await page.goto('/databases')
  await rowWith(page, 'demo').getByRole('link', { name: 'Schema' }).click()
  await page.getByRole('list', { name: 'Bagian database' }).getByRole('link', { name: 'Schema diff' }).click()
  await page.getByLabel('Database pembanding').selectOption({ label: 'gudang' })
  await page.getByRole('button', { name: 'Bandingkan' }).click()
  await expect(page.getByText(/Tidak ada perbedaan|Perbedaan/).first()).toBeVisible()
  await context.close()
})

test('editor memuat editor SQL dengan gaya yang tidak diblokir CSP', async ({ browser }) => {
  const { page, context, problems } = await open(browser, 'editor')
  await page.goto('/databases')
  const first = page.getByRole('row').filter({ has: page.getByRole('link', { name: 'Schema' }) }).first()
  await first.getByRole('link', { name: 'Schema' }).click()
  const migrationTab = page.getByRole('list', { name: 'Bagian database' }).getByRole('link', { name: 'Migration' })
  test.skip((await migrationTab.count()) === 0, 'editor tidak punya akses workflow')
  await page.goto(page.url().replace(/\/schema.*$/, '/migrations/new'))
  const editor = page.getByRole('textbox', { name: 'SQL up', exact: true })
  await expect(editor).toBeVisible()
  const gutter = await page.locator('.cm-gutters').first().evaluate((element) => getComputedStyle(element).display)
  expect(gutter).not.toBe('none')
  const family = await page.locator('.cm-scroller').first().evaluate((element) => getComputedStyle(element).fontFamily)
  expect(family).toMatch(/mono|Consolas|Courier/i)
  expect(problems.filter((problem) => /Content Security Policy|style-src|pageerror/i.test(problem))).toEqual([])
  await context.close()
})
