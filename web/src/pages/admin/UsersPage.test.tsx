// @vitest-environment jsdom
import { screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { handlers } from '../../mocks/handlers/index.ts'
import { demoPassword } from '../../mocks/seed.ts'
import { getState } from '../../mocks/state.ts'
import { setupMockServer } from '../../mocks/testing.ts'
import { openAs } from '../../test/app.tsx'
import { interceptFetch } from '../../test/network.ts'

setupMockServer(...handlers)

const longPassword = 'kata-sandi-panjang-sekali'

const table = async () => within(await screen.findByRole('table', { name: 'Daftar user' }))
const rowOf = async (username: string) => (await table()).getByText(username).closest('tr') as HTMLElement
const usernames = async () => (await table()).getAllByRole('row').slice(1).map((row) => within(row).getAllByRole('cell')[0].textContent?.replace('(Anda)', '').trim())

describe('daftar user', () => {
  it('menampilkan semua user terurut menurut username dengan status dan peran', async () => {
    openAs('admin', '/admin/users')
    expect(await usernames()).toEqual(['admin', 'auditor', 'editor', 'nonaktif', 'observer', 'pengamat', 'reviewer'])
    const admin = await rowOf('admin')
    expect(within(admin).getByText('Aktif')).toBeInTheDocument()
    expect(within(admin).getByText('Admin platform')).toBeInTheDocument()
    const nonaktif = await rowOf('nonaktif')
    expect(within(nonaktif).getByText('Nonaktif')).toBeInTheDocument()
    expect(within(nonaktif).getByText('User')).toBeInTheDocument()
  })

  it('akun sendiri diberi penanda dan tidak bisa dinonaktifkan', async () => {
    openAs('admin', '/admin/users')
    const admin = await rowOf('admin')
    expect(within(admin).getByText('(Anda)')).toBeInTheDocument()
    expect(within(admin).getByRole('button', { name: 'Nonaktifkan' })).toBeDisabled()
    expect(within(await rowOf('editor')).getByRole('button', { name: 'Nonaktifkan' })).toBeEnabled()
  })

  it('user biasa mendapat halaman akses ditolak', async () => {
    openAs('reviewer', '/admin/users')
    expect(await screen.findByText(/akses ditolak/i)).toBeInTheDocument()
    expect(screen.queryByRole('table', { name: 'Daftar user' })).not.toBeInTheDocument()
  })

  it('daftar panjang dibagi per halaman dan tautan halamannya tersimpan di URL', async () => {
    for (let index = 0; index < 55; index++) {
      getState().users.push({ id: crypto.randomUUID(), username: `zz${String(index).padStart(3, '0')}`, password: demoPassword, active: true, platform_admin: false })
    }
    const { user } = openAs('admin', '/admin/users')
    expect(await usernames()).toHaveLength(50)
    expect(screen.getByRole('button', { name: /Sebelumnya/ })).toBeDisabled()
    await user.click(screen.getByRole('link', { name: /Berikutnya/ }))
    expect(await screen.findByText('Halaman 2')).toBeInTheDocument()
    expect(await usernames()).toHaveLength(12)
    expect(screen.getByRole('button', { name: /Berikutnya/ })).toBeDisabled()
    await user.click(screen.getByRole('link', { name: /Sebelumnya/ }))
    expect(await screen.findByText('Halaman 1')).toBeInTheDocument()
  })

  it('halaman di luar jangkauan menawarkan kembali ke halaman pertama', async () => {
    const { user } = openAs('admin', '/admin/users?page=9')
    expect(await screen.findByText(/Tidak ada user di halaman ini/)).toBeInTheDocument()
    await user.click(screen.getByRole('link', { name: 'Kembali ke halaman pertama' }))
    expect(await usernames()).toHaveLength(7)
  })
})

// Server sungguhan membalas konflik dengan pesan umum sehingga tampilannya tidak boleh bergantung pada teks itu
function genericConflict(path: string) {
  const original = globalThis.fetch
  vi.spyOn(globalThis, 'fetch').mockImplementation(async (input, init) => {
    if (String(input).includes(path) && init?.method === 'POST') {
      return new Response(JSON.stringify({ error: { code: 'conflict', message: 'Data atau status berubah, silakan muat ulang', request_id: 'r' } }), { status: 409, headers: { 'Content-Type': 'application/json' } })
    }
    return original(input, init)
  })
}

describe('tambah user', () => {
  async function fill(user: ReturnType<typeof openAs>['user'], username: string, password: string) {
    await screen.findByRole('table', { name: 'Daftar user' })
    if (username) await user.type(screen.getByLabelText('Username'), username)
    if (password) await user.type(screen.getByLabelText('Password awal'), password)
  }

  it('membuat user dengan username dinormalkan lalu form dikosongkan', async () => {
    const { user } = openAs('admin', '/admin/users')
    await fill(user, '  Budi_01 ', longPassword)
    await user.click(screen.getByLabelText('Jadikan admin platform'))
    await user.click(screen.getByRole('button', { name: 'Tambah user' }))
    expect(await screen.findByRole('status')).toHaveTextContent('User budi_01 berhasil ditambahkan.')
    const budi = await rowOf('budi_01')
    expect(within(budi).getByText('Admin platform')).toBeInTheDocument()
    expect(screen.getByLabelText('Username')).toHaveValue('')
    expect(screen.getByLabelText('Password awal')).toHaveValue('')
    expect(screen.getByLabelText('Jadikan admin platform')).not.toBeChecked()
    expect(screen.getByLabelText('Username')).toHaveFocus()
  })

  it('isian kosong ditolak di browser tanpa mengirim request dan fokus ke isian pertama', async () => {
    const spy = interceptFetch('/admin/users')
    const { user } = openAs('admin', '/admin/users')
    await screen.findByRole('table', { name: 'Daftar user' })
    await user.click(screen.getByRole('button', { name: 'Tambah user' }))
    expect(screen.getByText('Username wajib diisi')).toBeInTheDocument()
    expect(screen.getByText('Password wajib diisi')).toBeInTheDocument()
    expect(screen.getByLabelText('Username')).toHaveFocus()
    expect(screen.getByLabelText('Username')).toHaveAttribute('aria-invalid', 'true')
    expect(spy.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(0)
  })

  it('password pendek ditolak dan pesannya hilang saat diketik ulang', async () => {
    const { user } = openAs('admin', '/admin/users')
    await fill(user, 'budi', 'pendek')
    await user.click(screen.getByRole('button', { name: 'Tambah user' }))
    expect(screen.getByText('Password minimal 15 karakter')).toBeInTheDocument()
    expect(screen.getByLabelText('Password awal')).toHaveFocus()
    await user.type(screen.getByLabelText('Password awal'), 'x')
    expect(screen.queryByText('Password minimal 15 karakter')).not.toBeInTheDocument()
  })

  it('username tidak valid dijelaskan aturannya', async () => {
    const { user } = openAs('admin', '/admin/users')
    await fill(user, 'ab', longPassword)
    await user.click(screen.getByRole('button', { name: 'Tambah user' }))
    expect(screen.getByText(/Username harus 3-32 karakter/)).toBeInTheDocument()
  })

  it('username yang sudah dipakai ditampilkan di bawah isiannya dan isian tetap terjaga', async () => {
    const { user } = openAs('admin', '/admin/users')
    await fill(user, 'reviewer', longPassword)
    await user.click(screen.getByRole('button', { name: 'Tambah user' }))
    expect(await screen.findByText('Username sudah dipakai')).toBeInTheDocument()
    expect(screen.getByLabelText('Username')).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByLabelText('Username')).toHaveValue('reviewer')
    expect(screen.getByLabelText('Password awal')).toHaveValue(longPassword)
  })

  it('konflik dengan pesan server yang umum tetap ditampilkan sebagai username sudah dipakai', async () => {
    genericConflict('/admin/users')
    const { user } = openAs('admin', '/admin/users')
    await fill(user, 'budi', longPassword)
    await user.click(screen.getByRole('button', { name: 'Tambah user' }))
    expect(await screen.findByText('Username sudah dipakai')).toBeInTheDocument()
    expect(screen.queryByText(/Data atau status berubah/)).not.toBeInTheDocument()
  })

  it('tombol mata menampilkan dan menyembunyikan password', async () => {
    const { user } = openAs('admin', '/admin/users')
    await fill(user, '', longPassword)
    const input = screen.getByLabelText('Password awal')
    expect(input).toHaveAttribute('type', 'password')
    await user.click(screen.getByRole('button', { name: 'Tampilkan password' }))
    expect(input).toHaveAttribute('type', 'text')
    await user.click(screen.getByRole('button', { name: 'Sembunyikan password' }))
    expect(input).toHaveAttribute('type', 'password')
  })

  it('kegagalan jaringan ditampilkan sebagai notifikasi dan isian tidak hilang', async () => {
    const { user } = openAs('admin', '/admin/users')
    await fill(user, 'budi', longPassword)
    interceptFetch('/admin/users', { failures: 1 })
    await user.click(screen.getByRole('button', { name: 'Tambah user' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Tidak dapat terhubung ke server')
    expect(screen.getByLabelText('Username')).toHaveValue('budi')
  })
})

describe('mengubah user', () => {
  it('menonaktifkan user setelah konfirmasi', async () => {
    const { user } = openAs('admin', '/admin/users')
    await user.click(within(await rowOf('editor')).getByRole('button', { name: 'Nonaktifkan' }))
    const dialog = screen.getByRole('dialog', { name: 'Nonaktifkan user' })
    expect(dialog).toHaveTextContent('editor')
    await user.click(within(dialog).getByRole('button', { name: 'Nonaktifkan' }))
    expect(await screen.findByRole('status')).toHaveTextContent('User editor dinonaktifkan')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(within(await rowOf('editor')).getByText('Nonaktif')).toBeInTheDocument()
    expect(getState().users.find((candidate) => candidate.username === 'editor')?.active).toBe(false)
  })

  it('membatalkan konfirmasi tidak mengubah apa pun', async () => {
    const { user } = openAs('admin', '/admin/users')
    await user.click(within(await rowOf('editor')).getByRole('button', { name: 'Nonaktifkan' }))
    await user.click(screen.getByRole('button', { name: 'Batal' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(within(await rowOf('editor')).getByText('Aktif')).toBeInTheDocument()
  })

  it('mengaktifkan kembali user nonaktif tanpa konfirmasi', async () => {
    const { user } = openAs('admin', '/admin/users')
    await user.click(within(await rowOf('nonaktif')).getByRole('button', { name: 'Aktifkan' }))
    expect(await screen.findByRole('status')).toHaveTextContent('User nonaktif diaktifkan')
    expect(within(await rowOf('nonaktif')).getByText('Aktif')).toBeInTheDocument()
  })

  it('kegagalan saat menonaktifkan ditampilkan di dalam dialog yang tetap terbuka', async () => {
    const { user } = openAs('admin', '/admin/users')
    await user.click(within(await rowOf('editor')).getByRole('button', { name: 'Nonaktifkan' }))
    interceptFetch('/admin/users/', { failures: 1 })
    const dialog = screen.getByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Nonaktifkan' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('Tidak dapat terhubung ke server')
    expect(within(await rowOf('editor')).getByText('Aktif')).toBeInTheDocument()
  })

  it('mereset password dan password lama tidak berlaku lagi', async () => {
    const { user } = openAs('admin', '/admin/users')
    await user.click(within(await rowOf('editor')).getByRole('button', { name: 'Reset password' }))
    const dialog = screen.getByRole('dialog', { name: 'Reset password' })
    expect(within(dialog).getByLabelText('Password baru')).toHaveFocus()
    await user.type(within(dialog).getByLabelText('Password baru'), 'pendek')
    await user.click(within(dialog).getByRole('button', { name: 'Reset password' }))
    expect(within(dialog).getByText('Password minimal 15 karakter')).toBeInTheDocument()
    await user.type(within(dialog).getByLabelText('Password baru'), '-sekarang-panjang')
    await user.click(within(dialog).getByRole('button', { name: 'Reset password' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Password editor berhasil direset')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(getState().users.find((candidate) => candidate.username === 'editor')?.password).toBe('pendek-sekarang-panjang')
  })

  it('dialog reset password bisa ditutup dengan Escape dan fokus kembali ke tombolnya', async () => {
    const { user } = openAs('admin', '/admin/users')
    const button = within(await rowOf('editor')).getByRole('button', { name: 'Reset password' })
    await user.click(button)
    await user.keyboard('{Escape}')
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(button).toHaveFocus()
  })
})
