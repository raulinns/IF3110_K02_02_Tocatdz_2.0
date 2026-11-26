import { expect, test } from '@playwright/test'
import { openTab, rowWith, typeSql } from './support'
import { approveAndExecute, approveAs, executeAs, history, step } from './real-flow'

// Batch yang berhenti di tengah dan pemulihannya terhadap server Go sungguhan
// Tabel 150 baris dengan CHECK yang hanya gagal untuk id di atas 100 membuat batch pertama (100 baris) berhasil dan batch kedua gagal sehingga job berstatus sebagian
test.skip(!process.env.E2E_BASE_URL, 'khusus server sungguhan')
test.describe.configure({ mode: 'serial' })

// State server tersimpan antar run sehingga nama dibuat unik, dan tanpa angka karena server menampilkan statement bernomor sebagai [REDACTED SQL]
const stamp = Date.now().toString(36).replace(/\d/g, (digit) => 'abcdefghij'[Number(digit)])
const table = `bt_${stamp}`
const setupTitle = `Siapkan ${table}`
const batchTitle = `Batch ${table}`
const rows = Array.from({ length: 150 }, (_, index) => `(${index + 1})`).join(', ')

async function createDraft(page: Parameters<Parameters<typeof step>[2]>[0], title: string) {
  await openTab(page, 'Migration')
  await page.getByRole('link', { name: 'Buat migration' }).click()
  await page.getByLabel('Judul').fill(title)
}

async function saveAndSubmit(page: Parameters<Parameters<typeof step>[2]>[0]) {
  await page.getByRole('button', { name: 'Simpan draft' }).click()
  await expect(page.getByText('Draft berhasil dibuat.')).toBeVisible()
  await page.getByRole('button', { name: 'Ajukan untuk review' }).click()
  await page.getByRole('dialog', { name: 'Ajukan untuk review' }).getByRole('button', { name: 'Ajukan' }).click()
  await expect(page.getByText(/Permintaan review dibuat/)).toBeVisible()
}

// Membuka halaman monitor job untuk migration batch dengan status tertentu lewat daftar job
async function openBatchJob(page: Parameters<Parameters<typeof step>[2]>[0], status: string) {
  await openTab(page, 'Monitor')
  await expect(page.getByRole('table', { name: 'Daftar job' })).toBeVisible()
  await rowWith(page, batchTitle, status).first().getByRole('link', { name: batchTitle }).click()
  await expect(page.getByRole('list', { name: 'Tahapan job' })).toBeVisible()
}

test('editor menyiapkan tabel 150 baris dan menjalankannya sampai selesai', async ({ browser }) => {
  await step(browser, 'editor', async (page) => {
    await createDraft(page, setupTitle)
    await typeSql(page, 'SQL up', `CREATE TABLE public.${table} (id integer PRIMARY KEY, status text NOT NULL DEFAULT 'baru', CONSTRAINT cek_${stamp} CHECK (id <= 100 OR status <> 'dirubah')); INSERT INTO public.${table} (id) VALUES ${rows};`)
    await typeSql(page, 'SQL down', `DROP TABLE public.${table};`)
    await saveAndSubmit(page)
  })
  await approveAndExecute(browser, setupTitle, 'Terapkan')
})

test('editor membuat migration batch lewat form dan mengajukannya', async ({ browser }) => {
  await step(browser, 'editor', async (page) => {
    await createDraft(page, batchTitle)
    await page.getByRole('radio', { name: 'Batch' }).check()
    await page.getByLabel('Schema', { exact: true }).fill('public')
    await page.getByLabel('Tabel', { exact: true }).fill(table)
    await page.getByLabel('Kolom kunci').fill('id')
    await page.getByLabel('Ukuran batch').fill('100')
    await typeSql(page, 'Bagian SET (up)', "status = 'dirubah'")
    await typeSql(page, 'Bagian SET (down)', "status = 'baru'")
    await saveAndSubmit(page)
  })
})

test('batch berhenti di batch kedua sehingga job berstatus sebagian dengan progres dan galat yang jelas', async ({ browser }) => {
  await approveAndExecute(browser, batchTitle, 'Terapkan', 'Sebagian')
  await step(browser, 'editor', async (page) => {
    await openBatchJob(page, 'Sebagian')
    await expect(page.getByRole('alert')).toContainText('SQLSTATE 23514')
    await expect(page.getByText('Batch di-commit').locator('xpath=following-sibling::dd[1]')).toHaveText('1')
    await expect(page.getByText('Baris di-commit').locator('xpath=following-sibling::dd[1]')).toHaveText('100')
    await expect(page.getByRole('button', { name: 'Resume' })).toBeEnabled()
    await expect(page.getByRole('button', { name: 'Revert batches' })).toBeEnabled()
  })
})

test('database ditahan job sebagian sehingga rollback dan forward dikunci', async ({ browser }) => {
  await step(browser, 'editor', async (page) => {
    await openTab(page, 'Riwayat')
    await expect(history(page)).toBeVisible()
    await expect(page.getByText(/Database sedang ditahan oleh/)).toBeVisible()
    await expect(page.getByRole('button', { name: 'Rollback', exact: true })).toBeDisabled()
    await expect(page.getByRole('button', { name: 'Forward', exact: true })).toBeDisabled()
  })
})

test('pengamat dengan akses lihat saja diminta menghubungi yang berhak memulihkan', async ({ browser }) => {
  await step(browser, 'pengamat', async (page) => {
    await openBatchJob(page, 'Sebagian')
    await expect(page.getByText(/perlu dipulihkan oleh pengguna dengan akses ubah/)).toBeVisible()
    await expect(page.getByRole('button', { name: 'Resume' })).toHaveCount(0)
  })
})

test('revert batch diajukan dari job, disetujui semua leader, dan dijalankan sampai berhasil', async ({ browser }) => {
  await step(browser, 'editor', async (page) => {
    await openBatchJob(page, 'Sebagian')
    await page.getByRole('button', { name: 'Revert batches' }).click()
    await page.getByRole('dialog', { name: 'Revert batch' }).getByRole('button', { name: 'Revert batch' }).click()
    await expect(page.getByText('Permintaan pemulihan dibuat dan menunggu approval.')).toBeVisible()
  })
  await approveAs(browser, 'pengamat', batchTitle, 'Revert batch')
  await approveAs(browser, 'admin', batchTitle, 'Revert batch')
  await executeAs(browser, batchTitle, 'Revert batch')
  await step(browser, 'editor', async (page) => {
    await openTab(page, 'Monitor')
    const revert = rowWith(page, batchTitle, 'Revert batch')
    await expect(revert.first()).toContainText('Berhasil')
  })
})

test('setelah dipulihkan job asal tidak lagi menawarkan pemulihan dan database tidak ditahan', async ({ browser }) => {
  await step(browser, 'editor', async (page) => {
    await openBatchJob(page, 'Sebagian')
    await expect(page.getByText(/sudah dipulihkan sehingga database tidak lagi ditahan/)).toBeVisible()
    await expect(page.getByRole('button', { name: 'Resume' })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Revert batches' })).toHaveCount(0)
    await openTab(page, 'Riwayat')
    await expect(history(page)).toBeVisible()
    await expect(page.getByText(/Database sedang ditahan oleh/)).toHaveCount(0)
  })
})
