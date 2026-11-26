import { expect, test, type APIRequestContext, type Locator, type Page } from '@playwright/test'
import { expectNoHorizontalScroll } from './support'
import { openAs, type Role } from './real-support'

// Setiap halaman diukur pada lebar 360 dan 1440 px dengan data nyata dari server, termasuk job sebagian, permintaan, dan riwayat batch
// Data dibuat oleh real-batch.spec.ts yang berjalan lebih dulu karena berkas diurutkan menurut nama
test.skip(!process.env.E2E_BASE_URL, 'khusus server sungguhan')

const widths = [360, 1440]

interface Ids {
  database: string
  other: string
  migration: string
  request: string
  job: string
}

async function get<T>(request: APIRequestContext, path: string): Promise<T> {
  const response = await request.get(`/api/v1${path}`)
  expect(response.status(), `GET ${path}`).toBe(200)
  return (await response.json()) as T
}

// ID dicari lewat API dengan sesi peran itu, dan job sebagian dipilih supaya halaman monitor memuat galat dan tombol pemulihan
async function lookup(request: APIRequestContext): Promise<Ids> {
  const databases = await get<{ items: { id: string; name: string }[] }>(request, '/databases?page=1&limit=50')
  const demo = databases.items.find((item) => item.name === 'demo')
  const other = databases.items.find((item) => item.name === 'gudang')
  expect(demo && other, 'database demo dan gudang').toBeTruthy()
  const base = `/databases/${demo!.id}`
  const migrations = await get<{ items: { id: string }[] }>(request, `${base}/migrations?page=1&limit=1`)
  const requests = await get<{ items: { id: string }[] }>(request, `${base}/change-requests?page=1&limit=1`)
  const jobs = await get<{ items: { id: string; status: string }[] }>(request, `${base}/jobs?page=1&limit=50`)
  expect(migrations.items.length && requests.items.length && jobs.items.length, 'jalankan real-batch.spec.ts lebih dulu supaya ada migration, permintaan, dan job').toBeTruthy()
  return {
    database: demo!.id,
    other: other!.id,
    migration: migrations.items[0].id,
    request: requests.items[0].id,
    job: (jobs.items.find((job) => job.status === 'partial') ?? jobs.items[0]).id,
  }
}

interface Screen {
  name: string
  role: Role
  path: (ids: Ids) => string
  ready: (page: Page) => Locator
  // Langkah tambahan supaya isi yang paling lebar ikut tampil
  prepare?: (page: Page) => Promise<void>
}

const table = (name: string) => (page: Page) => page.getByRole('table', { name })
const heading = (name: string) => (page: Page) => page.getByRole('heading', { name, level: 2 }).first()

const screens: Screen[] = [
  { name: 'daftar database', role: 'editor', path: () => '/databases', ready: table('Database yang dapat Anda akses') },
  { name: 'schema', role: 'editor', path: (ids) => `/databases/${ids.database}/schema`, ready: (page) => page.getByRole('link', { name: 'pelanggan' }).first() },
  { name: 'detail tabel', role: 'editor', path: (ids) => `/databases/${ids.database}/schema/public/pelanggan`, ready: heading('Kolom') },
  { name: 'data baris', role: 'editor', path: (ids) => `/databases/${ids.database}/schema/public/pelanggan/data`, ready: (page) => page.getByRole('region', { name: /Isi tabel/ }) },
  { name: 'daftar migration', role: 'editor', path: (ids) => `/databases/${ids.database}/migrations`, ready: table('Daftar migration') },
  {
    name: 'draft baru mode batch',
    role: 'editor',
    path: (ids) => `/databases/${ids.database}/migrations/new`,
    ready: (page) => page.getByRole('textbox', { name: 'SQL up', exact: true }),
    prepare: async (page) => {
      await page.getByRole('radio', { name: 'Batch' }).check()
      await expect(page.getByLabel('Kolom kunci')).toBeVisible()
    },
  },
  { name: 'detail migration', role: 'editor', path: (ids) => `/databases/${ids.database}/migrations/${ids.migration}`, ready: (page) => page.getByRole('textbox', { name: /Bagian SET \(up\)|SQL up/ }).first() },
  { name: 'schema diff', role: 'editor', path: (ids) => `/databases/${ids.database}/schema-diff?desired=${ids.other}&current_schema=public&desired_schema=public`, ready: (page) => page.getByText(/Tidak ada perbedaan|Perbedaan/).first() },
  { name: 'daftar review', role: 'editor', path: (ids) => `/databases/${ids.database}/review`, ready: table('Daftar permintaan perubahan') },
  { name: 'detail review', role: 'editor', path: (ids) => `/databases/${ids.database}/review/${ids.request}`, ready: table('Approver per group') },
  { name: 'daftar job', role: 'editor', path: (ids) => `/databases/${ids.database}/jobs`, ready: table('Daftar job') },
  { name: 'monitor job sebagian', role: 'editor', path: (ids) => `/databases/${ids.database}/jobs/${ids.job}`, ready: table('Percobaan job') },
  {
    name: 'riwayat dengan SQL terbuka',
    role: 'editor',
    path: (ids) => `/databases/${ids.database}/history`,
    ready: table('Riwayat eksekusi'),
    prepare: async (page) => {
      await page.getByRole('button', { name: 'SQL' }).first().click()
      await expect(page.getByRole('region', { name: 'SQL yang dijalankan' })).toBeVisible()
    },
  },
  { name: 'admin user', role: 'admin', path: () => '/admin/users', ready: table('Daftar user') },
  { name: 'admin group', role: 'admin', path: () => '/admin/groups', ready: table('Daftar group') },
  { name: 'admin koneksi', role: 'admin', path: () => '/admin/connections', ready: table('Daftar koneksi database') },
  { name: 'admin akses', role: 'admin', path: () => '/admin/grants', ready: table('Daftar akses database') },
]

for (const screen of screens) {
  test(`${screen.name} tidak punya scroll horizontal pada lebar 360 dan 1440 px`, async ({ browser }) => {
    const { page, context, problems } = await openAs(browser, screen.role)
    try {
      const ids = await lookup(context.request)
      for (const width of widths) {
        await page.setViewportSize({ width, height: 800 })
        await page.goto(screen.path(ids))
        await expect(screen.ready(page), `${screen.name} pada lebar ${width}`).toBeVisible()
        await screen.prepare?.(page)
        // Isi yang dimuat bertahap diberi waktu mengendap sebelum lebarnya diukur
        await page.waitForTimeout(400)
        await expectNoHorizontalScroll(page)
      }
      expect(problems).toEqual([])
    } finally {
      await context.close()
    }
  })
}
