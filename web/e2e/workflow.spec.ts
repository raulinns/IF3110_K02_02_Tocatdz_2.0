import { expect, test, type Page } from '@playwright/test'
import { enterDatabase, openTab, rowWith, startAs, switchTo, typeSql, type Account } from './support'

const title = 'Smoke tambah tabel e2e'

// Membuka permintaan lewat daftar Review berdasarkan jenis dan statusnya
async function openRequest(page: Page, kind: string, status: string) {
  await openTab(page, 'Review')
  await rowWith(page, title, kind, status).first().getByRole('link', { name: title }).click()
  await expect(page.getByRole('table', { name: 'Approver per group' })).toBeVisible()
}

// Leader memutuskan atas nama groupnya lalu hasilnya terlihat di tabel approver
async function approveAs(page: Page, leader: Account, kind: string, group: string) {
  await switchTo(page, leader)
  await enterDatabase(page)
  await openRequest(page, kind, 'Menunggu review')
  await page.getByRole('button', { name: 'Putuskan' }).click()
  const dialog = page.getByRole('dialog', { name: /Keputusan untuk group/ })
  await dialog.getByLabel('Catatan (opsional)').fill(`Disetujui ${leader}`)
  await dialog.getByRole('button', { name: 'Setujui' }).click()
  await expect(page.getByText(`Anda menyetujui atas nama group ${group}.`)).toBeVisible()
}

// Pengaju menjalankan permintaan yang sudah disetujui lalu menunggu job selesai lewat polling
async function executeAs(page: Page, kind: string) {
  await switchTo(page, 'editor')
  await enterDatabase(page)
  await openRequest(page, kind, 'Disetujui')
  await page.getByRole('button', { name: 'Jalankan', exact: true }).click()
  await page.getByRole('dialog', { name: 'Jalankan permintaan' }).getByRole('button', { name: 'Jalankan' }).click()
  const stepper = page.getByRole('list', { name: 'Tahapan job' })
  await expect(stepper).toBeVisible()
  await expect(stepper.getByRole('listitem').last()).toContainText('Berhasil', { timeout: 30_000 })
  await expect(page.getByRole('status').filter({ hasText: 'Pembaruan otomatis berhenti.' })).toBeVisible()
}

async function approveByBoth(page: Page, kind: string) {
  await approveAs(page, 'reviewer', kind, 'writers')
  await approveAs(page, 'observer', kind, 'readers')
  await expect(page.getByText(/Hanya pengguna dengan akses ubah/)).toBeVisible()
}

const history = (page: Page) => page.getByRole('table', { name: 'Riwayat eksekusi' })

test('login, buat migration, review oleh dua leader, jalankan, pantau, riwayat, rollback, dan forward', async ({ page }) => {
  await startAs(page, 'editor')
  await enterDatabase(page)

  // Membuat draft dengan mengetik SQL di editor lalu mengajukannya
  await openTab(page, 'Migration')
  await page.getByRole('link', { name: 'Buat migration' }).click()
  await page.getByLabel('Judul').fill(title)
  await typeSql(page, 'SQL up', 'CREATE TABLE IF NOT EXISTS public.e2e_smoke (id integer);')
  await typeSql(page, 'SQL down', 'DROP TABLE IF EXISTS public.e2e_smoke;')
  await page.getByRole('button', { name: 'Simpan draft' }).click()
  await expect(page.getByText('Draft berhasil dibuat.')).toBeVisible()
  await page.getByRole('button', { name: 'Ajukan untuk review' }).click()
  await page.getByRole('dialog', { name: 'Ajukan untuk review' }).getByRole('button', { name: 'Ajukan' }).click()
  await expect(page.getByText(/Permintaan review dibuat/)).toBeVisible()
  await expect(page.getByText(/Definisi dibekukan karena sudah diajukan/)).toBeVisible()

  // Pengaju tidak bisa menyetujui sendiri dan permintaan baru muncul di daftar Review
  await openTab(page, 'Review')
  await expect(rowWith(page, title, 'Terapkan', 'Menunggu review')).toBeVisible()

  await approveByBoth(page, 'Terapkan')
  await expect(rowWith(page, 'readers', 'Disetujui')).toBeVisible()

  await executeAs(page, 'Terapkan')

  // Riwayat menampilkan eksekusi baru sebagai posisi aktif
  await page.getByRole('link', { name: 'Lihat riwayat' }).click()
  await expect(history(page)).toBeVisible()
  const applied = rowWith(page, title, 'Terapkan').first()
  await expect(applied).toContainText('Aktif')
  await applied.getByRole('button', { name: 'SQL' }).click()
  await expect(page.getByRole('textbox', { name: 'Salinan SQL up' })).toContainText('CREATE TABLE IF NOT EXISTS public.e2e_smoke')

  // Rollback diajukan dari posisi database lalu disetujui dan dijalankan
  await page.getByRole('button', { name: 'Rollback', exact: true }).click()
  await page.getByRole('dialog', { name: 'Ajukan rollback' }).getByRole('button', { name: 'Ajukan rollback' }).click()
  await expect(page.getByText('Permintaan rollback dibuat dan menunggu approval.')).toBeVisible()
  await approveByBoth(page, 'Rollback')
  await executeAs(page, 'Rollback')
  await page.getByRole('link', { name: 'Lihat riwayat' }).click()
  const rolledBack = rowWith(page, title, 'Rollback')
  await expect(rolledBack.first()).toContainText('membatalkan')
  await expect(rowWith(page, title, 'Terapkan').first()).not.toContainText('Aktif')

  // Forward tersedia setelah rollback dan menjalankan kembali migration yang dibatalkan
  await expect(page.getByRole('button', { name: 'Forward', exact: true })).toBeEnabled()
  await page.getByRole('button', { name: 'Forward', exact: true }).click()
  await page.getByRole('dialog', { name: 'Ajukan forward' }).getByRole('button', { name: 'Ajukan forward' }).click()
  await expect(page.getByText('Permintaan forward dibuat dan menunggu approval.')).toBeVisible()
  await approveByBoth(page, 'Forward')
  await executeAs(page, 'Forward')
  await page.getByRole('link', { name: 'Lihat riwayat' }).click()
  await expect(rowWith(page, title, 'Forward').first()).toContainText('Aktif')

  // Dua pasang eksekusi dan rollback-nya disembunyikan, yaitu E2 dan E3 dari data contoh serta pasangan yang baru dibuat
  const total = await history(page).locator('tr[id^="execution-"]').count()
  // Kotak centang dikendalikan oleh URL sehingga statusnya baru berubah setelah transisi router dan bukan seketika setelah klik
  await page.getByLabel('Sembunyikan rollback').click()
  await expect(page.getByLabel('Sembunyikan rollback')).toBeChecked()
  await expect(history(page).locator('tr[id^="execution-"]')).toHaveCount(total - 4)
  await expect(page).toHaveURL(/hide_rollback=true/)
  await expect(rowWith(page, title, 'Rollback')).toHaveCount(0)
})
