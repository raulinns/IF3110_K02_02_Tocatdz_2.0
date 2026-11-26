import { expect, test } from '@playwright/test'
import { login, logout, password, startAs } from './support'

test.describe('masuk dan keluar', () => {
  test('masuk berhasil membuka daftar database dan keluar kembali ke halaman login', async ({ page }) => {
    await startAs(page, 'reviewer')
    await expect(page.getByRole('heading', { name: 'Database' })).toBeVisible()
    await expect(page.getByRole('table', { name: 'Database yang dapat Anda akses' })).toBeVisible()
    await logout(page)
    await expect(page).toHaveURL(/\/login/)
  })

  test('password salah menampilkan pesan generik yang sama untuk username yang tidak ada', async ({ page }) => {
    await page.goto('/login')
    await page.getByLabel('Username').fill('reviewer')
    await page.getByLabel('Password', { exact: true }).fill('salah-salah-salah')
    await page.getByRole('button', { name: 'Masuk' }).click()
    const salah = page.getByRole('alert')
    await expect(salah).toHaveText('Username atau password salah.')
    await page.getByLabel('Username').fill('tidak_ada')
    await page.getByLabel('Password', { exact: true }).fill(password)
    await page.getByRole('button', { name: 'Masuk' }).click()
    await expect(page.getByRole('alert')).toHaveText('Username atau password salah.')
    await expect(page).toHaveURL(/\/login/)
  })

  test('form kosong ditolak di browser dengan fokus ke isian pertama', async ({ page }) => {
    await page.goto('/login')
    await page.getByRole('button', { name: 'Masuk' }).click()
    await expect(page.getByText('Username wajib diisi')).toBeVisible()
    await expect(page.getByLabel('Username')).toBeFocused()
  })

  test('akun nonaktif tidak bisa masuk', async ({ page }) => {
    await page.goto('/login')
    await login(page, 'nonaktif')
    await expect(page.getByRole('alert')).toBeVisible()
    await expect(page).toHaveURL(/\/login/)
  })
})

test.describe('penjaga route', () => {
  test('halaman yang dilindungi mengalihkan ke login lalu kembali ke tujuan setelah masuk', async ({ page }) => {
    await page.goto('/admin/users')
    await expect(page).toHaveURL(/\/login/)
    await login(page, 'admin')
    await expect(page).toHaveURL(/\/admin\/users/)
    await expect(page.getByRole('table', { name: 'Daftar user' })).toBeVisible()
  })

  test('user biasa mendapat halaman akses ditolak di area admin', async ({ page }) => {
    await page.goto('/admin/users?as=reviewer')
    await expect(page.getByText('Akses ditolak')).toBeVisible()
  })

  test('tema gelap atau terang tersimpan setelah halaman dimuat ulang', async ({ page }) => {
    await page.goto('/login')
    const html = page.locator('html')
    const before = await html.getAttribute('data-bs-theme')
    await page.getByRole('button', { name: /tema/i }).click()
    const after = await html.getAttribute('data-bs-theme')
    expect(after).not.toBe(before)
    await page.reload()
    await expect(html).toHaveAttribute('data-bs-theme', after ?? '')
  })
})
