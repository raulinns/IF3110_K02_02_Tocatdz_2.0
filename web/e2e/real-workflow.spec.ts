import { expect, test } from '@playwright/test'
import { enterDatabase, openTab, rowWith, typeSql } from './support'
import { openAs } from './real-support'
import { approveAs, changePosition, database, executeAs, history, openRequest, step } from './real-flow'

// Alur workflow lengkap terhadap server Go sungguhan dengan sesi per peran: editor mengajukan, leader menyetujui, lalu worker menjalankan job
// Server harus memasang workflow dan seed harus memberi editor akses ubah ke database demo: E2E_BASE_URL=http://localhost:8080 E2E_ADMIN_PASSWORD=... E2E_VIEWER_PASSWORD=... E2E_EDITOR_PASSWORD=... npx playwright test --project=desktop
test.skip(!process.env.E2E_BASE_URL, 'khusus server sungguhan')
test.describe.configure({ mode: 'serial' })

// State server tersimpan antar run sehingga judul dan tabel dibuat unik
const stamp = Date.now().toString(36)
const title = `Uji nyata ${stamp}`
// Nama tabel tanpa angka karena server menampilkan statement yang memuat angka sebagai [REDACTED SQL]
const table = `uji_${stamp.replace(/\d/g, (digit) => 'abcdefghij'[Number(digit)])}`

test('editor membuat draft dengan editor SQL dan mengajukannya untuk review', async ({ browser }) => {
  await step(browser, 'editor', async (page) => {
    await openTab(page, 'Migration')
    await page.getByRole('link', { name: 'Buat migration' }).click()
    await page.getByLabel('Judul').fill(title)
    await typeSql(page, 'SQL up', `CREATE TABLE public.${table} (id integer PRIMARY KEY);`)
    await typeSql(page, 'SQL down', `DROP TABLE public.${table};`)
    await page.getByRole('button', { name: 'Simpan draft' }).click()
    await expect(page.getByText('Draft berhasil dibuat.')).toBeVisible()
    await page.getByRole('button', { name: 'Ajukan untuk review' }).click()
    await page.getByRole('dialog', { name: 'Ajukan untuk review' }).getByRole('button', { name: 'Ajukan' }).click()
    await expect(page.getByText(/Permintaan review dibuat/)).toBeVisible()
    await expect(page.getByText(/Definisi dibekukan karena sudah diajukan/)).toBeVisible()
  })
})

test('pengaju tidak bisa memutuskan permintaannya sendiri dan semua group approver terdaftar', async ({ browser }) => {
  await step(browser, 'editor', async (page) => {
    await openRequest(page, title, 'Terapkan', 'Menunggu review')
    await expect(page.getByRole('button', { name: 'Putuskan' })).toHaveCount(0)
    const approvers = page.getByRole('table', { name: 'Approver per group' })
    await expect(approvers).toContainText('pengembang')
    await expect(approvers).toContainText('pengamat')
  })
})

test('semua leader menyetujui sehingga permintaan berstatus disetujui', async ({ browser }) => {
  await approveAs(browser, 'pengamat', title, 'Terapkan')
  await approveAs(browser, 'admin', title, 'Terapkan')
  await step(browser, 'editor', async (page) => {
    await openTab(page, 'Review')
    await expect(rowWith(page, title, 'Terapkan', 'Disetujui')).toBeVisible()
  })
})

test('editor menjalankan permintaan lalu job dipantau sampai berhasil', async ({ browser }) => {
  await executeAs(browser, title, 'Terapkan')
})

test('riwayat menampilkan posisi aktif, salinan SQL, dan SQL yang dijalankan', async ({ browser }) => {
  await step(browser, 'editor', async (page) => {
    await openTab(page, 'Riwayat')
    await expect(history(page)).toBeVisible()
    const applied = rowWith(page, title, 'Terapkan').first()
    await expect(applied).toContainText('Aktif')
    await applied.getByRole('button', { name: 'SQL' }).click()
    await expect(page.getByRole('textbox', { name: 'Salinan SQL up' })).toContainText(`CREATE TABLE public.${table}`)
    const executed = page.getByRole('region', { name: 'SQL yang dijalankan' })
    await expect(executed).toContainText(`CREATE TABLE public.${table}`)
    await expect(executed).toContainText('[REDACTED]')
  })
})

test('rollback diajukan, disetujui semua leader, dijalankan, dan menggantikan posisi aktif', async ({ browser }) => {
  await changePosition(browser, title, 'Rollback')
  await step(browser, 'editor', async (page) => {
    await openTab(page, 'Riwayat')
    await expect(rowWith(page, title, 'Rollback').first()).toContainText('membatalkan')
    await expect(rowWith(page, title, 'Terapkan').first()).not.toContainText('Aktif')
    await expect(page.getByRole('button', { name: 'Forward', exact: true })).toBeEnabled()
  })
})

test('forward menjalankan kembali migration yang dibatalkan dan menjadi posisi aktif', async ({ browser }) => {
  await changePosition(browser, title, 'Forward')
  await step(browser, 'editor', async (page) => {
    await openTab(page, 'Riwayat')
    await expect(rowWith(page, title, 'Forward').first()).toContainText('Aktif')
  })
})

test('sembunyikan rollback menyembunyikan pasangan yang dibatalkan dan pilihannya tersimpan di URL', async ({ browser }) => {
  await step(browser, 'editor', async (page) => {
    await openTab(page, 'Riwayat')
    await expect(history(page)).toBeVisible()
    await expect(rowWith(page, title, 'Rollback')).toHaveCount(1)
    // Kotak centang dikendalikan oleh URL sehingga statusnya baru berubah setelah transisi router dan bukan seketika setelah klik
    await page.getByLabel('Sembunyikan rollback').click()
    await expect(page.getByLabel('Sembunyikan rollback')).toBeChecked()
    await expect(page).toHaveURL(/hide_rollback=true/)
    await expect(rowWith(page, title, 'Rollback')).toHaveCount(0)
    await expect(rowWith(page, title, 'Terapkan')).toHaveCount(0)
    await expect(rowWith(page, title, 'Forward').first()).toBeVisible()
    await page.reload()
    await expect(page.getByLabel('Sembunyikan rollback')).toBeChecked()
  })
})

test('pengamat dengan akses lihat saja melihat riwayat tanpa tombol rollback atau forward', async ({ browser }) => {
  const { page, context, problems } = await openAs(browser, 'pengamat')
  await page.goto('/databases')
  await enterDatabase(page, database)
  await page.getByRole('list', { name: 'Bagian database' }).getByRole('link', { name: 'Riwayat', exact: true }).click()
  await expect(history(page)).toBeVisible()
  await expect(page.getByRole('button', { name: 'Rollback', exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Forward', exact: true })).toHaveCount(0)
  expect(problems).toEqual([])
  await context.close()
})
