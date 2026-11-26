import { expect, test, type Locator, type Page } from '@playwright/test'
import { expectNoHorizontalScroll, type Account } from './support'

const inventaris = '10000000-0000-4000-8000-000000000001'
const pelaporan = '10000000-0000-4000-8000-000000000002'
const draft = '20000000-0000-4000-8000-000000000004'
const request = '50000000-0000-4000-8000-000000000005'
const job = '60000000-0000-4000-8000-000000000005'
const writers = '40000000-0000-4000-8000-000000000001'

interface Screen {
  name: string
  user: Account | null
  path: string
  ready: (page: Page) => Locator
}

const table = (name: string) => (page: Page) => page.getByRole('table', { name })
const heading = (name: string) => (page: Page) => page.getByRole('heading', { name, level: 2 }).first()

// Setiap halaman utama dibuka sebagai akun yang berhak, dan state mock dimuat ulang untuk tiap halaman karena hanya dibaca
const screens: Screen[] = [
  { name: 'login', user: null, path: '/login', ready: (page) => page.getByRole('button', { name: 'Masuk' }) },
  { name: 'daftar database', user: 'reviewer', path: '/databases', ready: table('Database yang dapat Anda akses') },
  { name: 'schema', user: 'reviewer', path: `/databases/${inventaris}/schema`, ready: (page) => page.getByRole('link', { name: 'orders' }).first() },
  { name: 'detail tabel', user: 'reviewer', path: `/databases/${inventaris}/schema/public/orders`, ready: heading('Kolom') },
  { name: 'data baris', user: 'reviewer', path: `/databases/${inventaris}/schema/public/orders/data`, ready: (page) => page.getByRole('region', { name: /Isi tabel/ }) },
  { name: 'daftar migration', user: 'reviewer', path: `/databases/${inventaris}/migrations`, ready: table('Daftar migration') },
  { name: 'draft baru', user: 'reviewer', path: `/databases/${inventaris}/migrations/new`, ready: (page) => page.getByRole('textbox', { name: 'SQL up', exact: true }) },
  { name: 'detail migration', user: 'reviewer', path: `/databases/${inventaris}/migrations/${draft}`, ready: heading('EXPLAIN') },
  { name: 'schema diff', user: 'reviewer', path: `/databases/${inventaris}/schema-diff?desired=${pelaporan}&current_schema=public&desired_schema=public`, ready: table('Perbedaan schema') },
  { name: 'daftar review', user: 'reviewer', path: `/databases/${inventaris}/review`, ready: table('Daftar permintaan perubahan') },
  { name: 'detail review', user: 'reviewer', path: `/databases/${inventaris}/review/${request}`, ready: table('Approver per group') },
  { name: 'daftar job', user: 'reviewer', path: `/databases/${inventaris}/jobs`, ready: table('Daftar job') },
  { name: 'monitor job', user: 'reviewer', path: `/databases/${inventaris}/jobs/${job}`, ready: (page) => page.getByRole('list', { name: 'Tahapan job' }) },
  { name: 'riwayat', user: 'reviewer', path: `/databases/${inventaris}/history`, ready: table('Riwayat eksekusi') },
  { name: 'admin user', user: 'admin', path: '/admin/users', ready: table('Daftar user') },
  { name: 'admin group', user: 'admin', path: '/admin/groups', ready: table('Daftar group') },
  { name: 'detail group', user: 'admin', path: `/admin/groups/${writers}`, ready: table('Anggota group writers') },
  { name: 'admin koneksi', user: 'admin', path: '/admin/connections', ready: table('Daftar koneksi database') },
  { name: 'admin akses', user: 'admin', path: '/admin/grants', ready: table('Daftar akses database') },
]

// Lebar yang diuji dibagi dua menurut profil supaya profil mobile memakai emulasi layar sentuh dan desktop memakai jendela biasa
const widths: Record<string, number[]> = { desktop: [768, 1024, 1440], mobile: [360, 390] }

for (const screen of screens) {
  test(`${screen.name} tidak punya scroll horizontal di semua lebar layar`, async ({ page }, info) => {
    for (const width of widths[info.project.name]) {
      await page.setViewportSize({ width, height: 800 })
      await page.goto(screen.user ? `${screen.path}${screen.path.includes('?') ? '&' : '?'}as=${screen.user}` : screen.path)
      await expect(screen.ready(page), `${screen.name} pada lebar ${width}`).toBeVisible()
      // Isi yang dimuat bertahap diberi waktu mengendap sebelum lebarnya diukur
      await page.waitForTimeout(300)
      await expectNoHorizontalScroll(page)
    }
  })
}

test.describe('navigasi di layar kecil', () => {
  // Playwright mewajibkan argumen pertama berupa pola destrukturisasi walau kosong
  // eslint-disable-next-line no-empty-pattern
  test.beforeEach(({}, info) => {
    test.skip(info.project.name !== 'mobile', 'khusus profil mobile')
  })

  test('sidebar tertutup lalu dibuka dengan tombol menu dan tertutup lagi setelah memilih tujuan', async ({ page }) => {
    await page.goto('/admin/users?as=admin')
    await expect(page.getByRole('table', { name: 'Daftar user' })).toBeVisible()
    const sidebar = page.locator('#sidebar')
    await expect(sidebar).not.toBeInViewport()
    await page.getByRole('button', { name: 'Tampilkan atau sembunyikan menu' }).click()
    await expect(sidebar).toBeInViewport()
    await page.getByRole('navigation', { name: 'Navigasi utama' }).getByRole('link', { name: 'Group' }).click()
    await expect(page.getByRole('table', { name: 'Daftar group' })).toBeVisible()
    await expect(sidebar).not.toBeInViewport()
  })

  test('latar gelap menutup sidebar dan tombol Keluar dapat dijangkau dengan nama yang jelas', async ({ page }) => {
    await page.goto('/databases?as=reviewer')
    await page.getByRole('button', { name: 'Tampilkan atau sembunyikan menu' }).click()
    await expect(page.locator('#sidebar')).toBeInViewport()
    await page.locator('.sidebar-backdrop').click({ position: { x: 380, y: 400 } })
    await expect(page.locator('#sidebar')).not.toBeInViewport()
    await expect(page.getByRole('button', { name: 'Keluar' })).toBeVisible()
  })
})

test.describe('dialog di layar kecil', () => {
  test('dialog konfirmasi berada di dalam layar dan semua tombolnya bisa ditekan', async ({ page }) => {
    await page.goto('/admin/users?as=admin')
    await expect(page.getByRole('table', { name: 'Daftar user' })).toBeVisible()
    await page.getByRole('row').filter({ hasText: 'editor' }).getByRole('button', { name: 'Nonaktifkan' }).click()
    const dialog = page.getByRole('dialog', { name: 'Nonaktifkan user' })
    await expect(dialog).toBeVisible()
    const box = await dialog.boundingBox()
    const viewport = page.viewportSize()!
    expect(box!.x).toBeGreaterThanOrEqual(0)
    expect(box!.x + box!.width).toBeLessThanOrEqual(viewport.width)
    await expectNoHorizontalScroll(page)
    await dialog.getByRole('button', { name: 'Batal' }).click()
    await expect(dialog).toBeHidden()
  })

  test('dialog tidak mengunci halaman setelah ditutup dengan Escape dan fokus kembali ke pemicunya', async ({ page }) => {
    await page.goto('/admin/users?as=admin')
    const trigger = page.getByRole('row').filter({ hasText: 'editor' }).getByRole('button', { name: 'Reset password' })
    await trigger.click()
    await expect(page.getByRole('dialog', { name: 'Reset password' })).toBeVisible()
    await page.keyboard.press('Escape')
    await expect(page.getByRole('dialog')).toBeHidden()
    await expect(trigger).toBeFocused()
    await expect(page.locator('body')).not.toHaveClass(/modal-open/)
  })
})
