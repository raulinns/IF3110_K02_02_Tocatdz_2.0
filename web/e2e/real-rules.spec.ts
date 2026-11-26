import { expect, test } from '@playwright/test'
import { openTab, rowWith, typeSql } from './support'
import { approveAndExecute, history, step } from './real-flow'

// Aturan bisnis M2 terhadap server Go sungguhan: migration irreversible, satu permintaan terbuka per database, dan akun lihat saja
test.skip(!process.env.E2E_BASE_URL, 'khusus server sungguhan')
test.describe.configure({ mode: 'serial' })

// Nama tanpa angka karena server menampilkan statement bernomor sebagai [REDACTED SQL]
const stamp = Date.now().toString(36).replace(/\d/g, (digit) => 'abcdefghij'[Number(digit)])
const irreversibleTitle = `Irreversible ${stamp}`
const waitingTitle = `Menunggu ${stamp}`
const table = `ir_${stamp}`

test('editor membuat migration irreversible dengan alasan dan mengajukannya', async ({ browser }) => {
  await step(browser, 'editor', async (page) => {
    await openTab(page, 'Migration')
    await page.getByRole('link', { name: 'Buat migration' }).click()
    await page.getByLabel('Judul').fill(irreversibleTitle)
    await typeSql(page, 'SQL up', `CREATE TABLE public.${table} (id integer PRIMARY KEY);`)
    await page.getByLabel('Irreversible (tidak dapat dibatalkan)').check()
    await page.getByLabel('Alasan irreversible').fill('Uji otomatis: perubahan ini sengaja tanpa SQL down')
    await expect(page.getByRole('textbox', { name: 'SQL down', exact: true })).toHaveCount(0)
    await page.getByRole('button', { name: 'Simpan draft' }).click()
    await expect(page.getByText('Draft berhasil dibuat.')).toBeVisible()
    await page.getByRole('button', { name: 'Ajukan untuk review' }).click()
    await page.getByRole('dialog', { name: 'Ajukan untuk review' }).getByRole('button', { name: 'Ajukan' }).click()
    await expect(page.getByText(/Permintaan review dibuat/)).toBeVisible()
  })
})

test('satu database hanya menerima satu permintaan terbuka sehingga pengajuan kedua ditolak dengan penjelasan', async ({ browser }) => {
  await step(browser, 'editor', async (page) => {
    await openTab(page, 'Migration')
    await page.getByRole('link', { name: 'Buat migration' }).click()
    await page.getByLabel('Judul').fill(waitingTitle)
    await typeSql(page, 'SQL up', `CREATE TABLE public.${table}_dua (id integer PRIMARY KEY);`)
    await typeSql(page, 'SQL down', `DROP TABLE public.${table}_dua;`)
    await page.getByRole('button', { name: 'Simpan draft' }).click()
    await expect(page.getByText('Draft berhasil dibuat.')).toBeVisible()
    await page.getByRole('button', { name: 'Ajukan untuk review' }).click()
    const dialog = page.getByRole('dialog', { name: 'Ajukan untuk review' })
    await dialog.getByRole('button', { name: 'Ajukan' }).click()
    await expect(dialog.getByRole('alert')).toContainText('Muat ulang lalu coba lagi')
    // Draft tetap bisa diubah karena pengajuan yang ditolak tidak membekukannya
    await dialog.getByRole('button', { name: 'Batal' }).click()
    await expect(page.getByRole('button', { name: 'Simpan draft' })).toBeVisible()
  }, [/^409 .*\/change-requests$/])
})

test('pengamat dengan akses lihat saja tidak melihat tombol buat migration dan tidak bisa menjalankan permintaan', async ({ browser }) => {
  await step(browser, 'pengamat', async (page) => {
    await openTab(page, 'Migration')
    await expect(page.getByRole('table', { name: 'Daftar migration' })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Buat migration' })).toHaveCount(0)
    await openTab(page, 'Review')
    await rowWith(page, irreversibleTitle, 'Menunggu review').first().getByRole('link', { name: irreversibleTitle }).click()
    await expect(page.getByRole('button', { name: 'Jalankan', exact: true })).toHaveCount(0)
  })
})

test('permintaan irreversible disetujui dan dijalankan sampai berhasil', async ({ browser }) => {
  await approveAndExecute(browser, irreversibleTitle, 'Terapkan')
})

test('rollback dikunci karena migration aktif irreversible dan alasannya dijelaskan', async ({ browser }) => {
  await step(browser, 'editor', async (page) => {
    await openTab(page, 'Riwayat')
    await expect(history(page)).toBeVisible()
    await expect(rowWith(page, irreversibleTitle, 'Terapkan').first()).toContainText('Aktif')
    await expect(page.getByText('Tidak tersedia: migration aktif ditandai irreversible')).toBeVisible()
    await expect(page.getByRole('button', { name: 'Rollback', exact: true })).toBeDisabled()
  })
})
