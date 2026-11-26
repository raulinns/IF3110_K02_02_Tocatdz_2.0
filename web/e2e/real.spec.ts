import { expect, test } from '@playwright/test'
import { rowWith } from './support'
import { openAs } from './real-support'

// Spec ini hanya berjalan terhadap server Go sungguhan: E2E_BASE_URL=http://localhost:8080 E2E_ADMIN_PASSWORD=... E2E_VIEWER_PASSWORD=... E2E_EDITOR_PASSWORD=... npx playwright test --project=desktop
// Tiap peran masuk satu kali di real-setup sehingga pembatas login server tidak tersentuh
test.skip(!process.env.E2E_BASE_URL, 'khusus server sungguhan')
test.describe.configure({ mode: 'serial' })

// State server tersimpan antar test sehingga nama dibuat unik per run
const group = `uji-${Date.now().toString(36)}`

test('halaman dimuat tanpa kesalahan konsol atau pelanggaran CSP', async ({ browser }) => {
  const { page, context, problems } = await openAs(browser, 'admin')
  await page.goto('/admin/users')
  await expect(page.getByRole('table', { name: 'Daftar user' })).toBeVisible()
  await page.goto('/admin/groups')
  await expect(page.getByRole('table', { name: 'Daftar group' })).toBeVisible()
  expect(problems).toEqual([])
  await context.close()
})

test('admin membuat group, menambah anggota, menetapkan leader, dan memberi akses ke database demo', async ({ browser }) => {
  const { page, context } = await openAs(browser, 'admin')
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
  const { page, context } = await openAs(browser, 'admin')
  await page.goto('/admin/groups')
  await page.getByLabel('Nama group').fill(group)
  await page.getByRole('button', { name: 'Buat group' }).click()
  await expect(page.getByText('Nama group sudah dipakai')).toBeVisible()
  await context.close()
})

test('pengamat menjelajah schema, tabel, dan data baris database demo', async ({ browser }) => {
  const { page, context, problems } = await openAs(browser, 'pengamat')
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
  const { page, context } = await openAs(browser, 'pengamat')
  await page.goto('/databases')
  await rowWith(page, 'demo').getByRole('link', { name: 'Schema' }).click()
  await page.getByRole('list', { name: 'Bagian database' }).getByRole('link', { name: 'Schema diff' }).click()
  await page.getByLabel('Database pembanding').selectOption({ label: 'gudang' })
  await page.getByRole('button', { name: 'Bandingkan' }).click()
  await expect(page.getByText(/Tidak ada perbedaan|Perbedaan/).first()).toBeVisible()
  await context.close()
})

test('editor memuat editor SQL dengan gaya yang tidak diblokir CSP', async ({ browser }) => {
  const { page, context, problems } = await openAs(browser, 'editor')
  await page.goto('/databases')
  await rowWith(page, 'demo').getByRole('link', { name: 'Schema' }).click()
  // Tab Migration wajib ada karena editor memiliki akses ubah, dan tes ini gagal bila tidak, bukan dilewati
  const migrationTab = page.getByRole('list', { name: 'Bagian database' }).getByRole('link', { name: 'Migration' })
  await expect(migrationTab).toBeVisible()
  await migrationTab.click()
  await page.getByRole('link', { name: 'Buat migration' }).click()
  const editor = page.getByRole('textbox', { name: 'SQL up', exact: true })
  await expect(editor).toBeVisible()
  const gutter = await page.locator('.cm-gutters').first().evaluate((element) => getComputedStyle(element).display)
  expect(gutter).not.toBe('none')
  const family = await page.locator('.cm-scroller').first().evaluate((element) => getComputedStyle(element).fontFamily)
  expect(family).toMatch(/mono|Consolas|Courier/i)
  expect(problems.filter((problem) => /Content Security Policy|style-src|pageerror/i.test(problem))).toEqual([])
  await context.close()
})

// Group uji dihapus di akhir supaya tidak menambah approver pada permintaan workflow run berikutnya, dan sekaligus menguji hapus beserta akses databasenya
test('admin menghapus group uji beserta keanggotaan dan aksesnya', async ({ browser }) => {
  const { page, context } = await openAs(browser, 'admin')
  await page.goto('/admin/groups')
  await rowWith(page, group).getByRole('link', { name: `Kelola group ${group}` }).click()
  await page.getByRole('button', { name: 'Hapus group' }).first().click()
  await page.getByRole('dialog', { name: 'Hapus group' }).getByRole('button', { name: 'Hapus group' }).click()
  await expect(page.getByRole('table', { name: 'Daftar group' })).toBeVisible()
  await expect(rowWith(page, group)).toHaveCount(0)
  await context.close()
})
