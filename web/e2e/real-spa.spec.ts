import { expect, test, type APIRequestContext, type Page } from '@playwright/test'
import { expectNoHorizontalScroll, openTab, rowWith, typeSql } from './support'
import { openAs } from './real-support'

// Perilaku SPA terhadap server sungguhan: tanpa reload penuh, deep link, koneksi putus, dialog, editor SQL, dan polling
// Membutuhkan data dari spec lain atau dari pengujian manual: minimal satu migration, satu permintaan, satu job, dan satu eksekusi
test.skip(!process.env.E2E_BASE_URL, 'khusus server sungguhan')

const widths = [360, 1440]

async function get<T>(request: APIRequestContext, path: string): Promise<T> {
  const response = await request.get(`/api/v1${path}`)
  expect(response.status(), `GET ${path}`).toBe(200)
  return (await response.json()) as T
}

async function ids(request: APIRequestContext) {
  const databases = await get<{ items: { id: string; name: string }[] }>(request, '/databases?page=1&limit=50')
  const database = databases.items.find((item) => item.name === 'demo')!.id
  const jobs = await get<{ items: { id: string; status: string }[] }>(request, `/databases/${database}/jobs?page=1&limit=50`)
  const migrations = await get<{ items: { id: string }[] }>(request, `/databases/${database}/migrations?page=1&limit=1`)
  const requests = await get<{ items: { id: string }[] }>(request, `/databases/${database}/change-requests?page=1&limit=1`)
  // Job yang sukses dipilih karena job gagal atau sebagian selalu menampilkan kotak galatnya sendiri
  const finished = jobs.items.find((job) => job.status === 'succeeded')
  expect(finished && migrations.items.length && requests.items.length, 'butuh minimal satu job selesai, migration, dan permintaan di database demo').toBeTruthy()
  return { database, job: finished!.id, migration: migrations.items[0].id, request: requests.items[0].id }
}

test('berpindah antar halaman internal tidak memuat ulang dokumen dan data datang dari API', async ({ browser }) => {
  const { page, context, problems } = await openAs(browser, 'editor')
  const documents: string[] = []
  page.on('request', (request) => {
    if (request.resourceType() === 'document') documents.push(request.url())
  })
  const apiCalls: string[] = []
  page.on('request', (request) => {
    if (new URL(request.url()).pathname.startsWith('/api/v1/')) apiCalls.push(new URL(request.url()).pathname)
  })
  await page.goto('/databases')
  await page.evaluate(() => {
    ;(window as unknown as { penanda: string }).penanda = 'sama'
  })
  const baseline = documents.length
  await rowWith(page, 'demo').getByRole('link', { name: 'Schema' }).click()
  for (const tab of ['Migration', 'Review', 'Monitor', 'Riwayat']) {
    await openTab(page, tab)
    await expect(page.getByRole('heading', { level: 2 }).first()).toBeVisible()
    expect(await page.evaluate(() => (window as unknown as { penanda?: string }).penanda), `penanda hilang setelah membuka ${tab}`).toBe('sama')
  }
  expect(documents.length - baseline, 'tidak boleh ada permintaan dokumen baru selama berpindah halaman').toBe(0)
  expect(apiCalls.some((path) => /\/(migrations|change-requests|jobs|executions|position)/.test(path))).toBe(true)
  expect(problems).toEqual([])
  await context.close()
})

test('muat ulang dan tautan langsung ke halaman detail tetap berfungsi', async ({ browser }) => {
  const { page, context, problems } = await openAs(browser, 'editor')
  const data = await ids(context.request)
  const pages: [string, () => ReturnType<Page['getByRole']>][] = [
    [`/databases/${data.database}/migrations/${data.migration}`, () => page.getByRole('heading', { name: 'EXPLAIN', level: 2 })],
    [`/databases/${data.database}/review/${data.request}`, () => page.getByRole('table', { name: 'Approver per group' })],
    [`/databases/${data.database}/jobs/${data.job}`, () => page.getByRole('list', { name: 'Tahapan job' })],
    [`/databases/${data.database}/history`, () => page.getByRole('table', { name: 'Riwayat eksekusi' })],
  ]
  for (const [path, ready] of pages) {
    await page.goto(path)
    await expect(ready(), path).toBeVisible()
    await page.reload()
    await expect(ready(), `${path} setelah refresh`).toBeVisible()
  }
  expect(problems).toEqual([])
  await context.close()
})

test('koneksi putus menampilkan pesan jelas, dipulihkan dengan Coba lagi, dan tidak menjalankan ulang apa pun', async ({ browser }) => {
  const { page, context, problems } = await openAs(browser, 'editor')
  const data = await ids(context.request)
  const writes: string[] = []
  page.on('request', (request) => {
    if (request.method() !== 'GET' && new URL(request.url()).pathname.startsWith('/api/v1/')) writes.push(`${request.method()} ${new URL(request.url()).pathname}`)
  })
  await page.goto(`/databases/${data.database}/jobs/${data.job}`)
  await expect(page.getByRole('list', { name: 'Tahapan job' })).toBeVisible()
  await context.setOffline(true)
  await page.getByRole('button', { name: 'Muat ulang' }).first().click()
  const offline = page.getByRole('alert').filter({ hasText: 'Tidak dapat terhubung' })
  await expect(offline).toBeVisible()
  await context.setOffline(false)
  await page.getByRole('button', { name: 'Coba lagi' }).click()
  await expect(page.getByRole('list', { name: 'Tahapan job' })).toBeVisible()
  await expect(offline).toHaveCount(0)
  expect(writes, 'tidak boleh ada permintaan tulis (misalnya execute) saat pulih').toEqual([])
  expect(problems.filter((problem) => !/Failed to fetch|net::ERR/i.test(problem))).toEqual([])
  await context.close()
})

test('polling berhenti pada status akhir dan tidak meminta job lagi', async ({ browser }) => {
  const { page, context } = await openAs(browser, 'editor')
  const data = await ids(context.request)
  await page.goto(`/databases/${data.database}/jobs/${data.job}`)
  await expect(page.getByRole('status').filter({ hasText: 'Pembaruan otomatis berhenti.' })).toBeVisible()
  let polls = 0
  page.on('request', (request) => {
    if (new URL(request.url()).pathname === `/api/v1/jobs/${data.job}`) polls += 1
  })
  await page.waitForTimeout(6000)
  expect(polls, 'job yang sudah final tidak boleh dipolling').toBe(0)
  await context.close()
})

for (const width of widths) {
  test(`dialog dan editor SQL terjangkau pada lebar ${width} px`, async ({ browser }) => {
    const { page, context, problems } = await openAs(browser, 'editor')
    const data = await ids(context.request)
    await page.setViewportSize({ width, height: 800 })

    // Dialog ajukan rollback dari kartu posisi: harus berada di dalam layar dan tombolnya bisa ditekan, tanpa mengirim apa pun
    await page.goto(`/databases/${data.database}/history`)
    await expect(page.getByRole('table', { name: 'Riwayat eksekusi' })).toBeVisible()
    const rollback = page.getByRole('button', { name: 'Rollback', exact: true })
    if (await rollback.isEnabled()) {
      await rollback.click()
      const dialog = page.getByRole('dialog', { name: 'Ajukan rollback' })
      await expect(dialog).toBeVisible()
      const box = (await dialog.boundingBox())!
      expect(box.x).toBeGreaterThanOrEqual(0)
      expect(box.x + box.width).toBeLessThanOrEqual(width + 1)
      await dialog.getByRole('button', { name: 'Batal' }).click()
      await expect(dialog).toBeHidden()
    }

    // Editor SQL dapat diisi dan halaman tidak melebar
    await page.goto(`/databases/${data.database}/migrations/new`)
    await page.getByLabel('Judul').fill(`Uji editor ${width}`)
    await typeSql(page, 'SQL up', 'SELECT 1;')
    await expectNoHorizontalScroll(page)
    await expect(page.getByRole('button', { name: 'Simpan draft' })).toBeVisible()

    expect(problems.filter((problem) => /Content Security Policy|pageerror|console:/i.test(problem))).toEqual([])
    await context.close()
  })
}
