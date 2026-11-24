// @vitest-environment jsdom
import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { handlers } from '../../mocks/handlers/index.ts'
import { ids } from '../../mocks/ids.ts'
import { getState } from '../../mocks/state.ts'
import { setupMockServer } from '../../mocks/testing.ts'
import { openAs } from '../../test/app.tsx'
import { interceptFetch } from '../../test/network.ts'

setupMockServer(...handlers)

const dsn = 'postgres://app:rahasia@db.internal:5432/baru'

const table = async () => within(await screen.findByRole('table', { name: 'Daftar koneksi database' }))
const rowOf = async (name: string) => (await table()).getByText(name).closest('tr') as HTMLElement

describe('daftar koneksi', () => {
  it('menampilkan koneksi terurut menurut nama dengan DBMS dan ID', async () => {
    openAs('admin', '/admin/connections')
    const names = (await table()).getAllByRole('row').slice(1).map((row) => within(row).getAllByRole('cell')[0].textContent?.trim())
    expect(names).toEqual(['arsip', 'inventaris', 'pelaporan'])
    const inventaris = await rowOf('inventaris')
    expect(within(inventaris).getByText('PostgreSQL')).toBeInTheDocument()
    expect(within(inventaris).getByText(ids.inventaris)).toBeInTheDocument()
    expect(within(await rowOf('arsip')).getByText('MySQL')).toBeInTheDocument()
  })

  it('user biasa mendapat halaman akses ditolak', async () => {
    openAs('observer', '/admin/connections')
    expect(await screen.findByText(/akses ditolak/i)).toBeInTheDocument()
  })

  it('bisa berpindah dari tab User ke tab Koneksi', async () => {
    const { user } = openAs('admin', '/admin/users')
    // Tautan yang sama ada di sidebar sehingga pencarian dibatasi pada tab admin
    await user.click(within(await screen.findByLabelText('Bagian admin')).getByRole('link', { name: 'Koneksi' }))
    expect(await screen.findByRole('table', { name: 'Daftar koneksi database' })).toBeInTheDocument()
  })
})

describe('tambah koneksi', () => {
  async function fill(user: ReturnType<typeof openAs>['user'], name: string, connection: string) {
    await screen.findByRole('table', { name: 'Daftar koneksi database' })
    if (name) await user.type(screen.getByLabelText('Nama tampilan'), name)
    if (connection) await user.type(screen.getByLabelText('Connection string'), connection)
  }

  it('menyimpan koneksi baru lalu form dikosongkan dan connection string tidak tampil lagi', async () => {
    const { user } = openAs('admin', '/admin/connections')
    await fill(user, '  baru  ', dsn)
    await user.selectOptions(screen.getByLabelText('DBMS'), 'postgresql')
    await user.click(screen.getByRole('button', { name: 'Simpan koneksi' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Koneksi baru berhasil disimpan.')
    expect(within(await rowOf('baru')).getByText('PostgreSQL')).toBeInTheDocument()
    expect(screen.getByLabelText('Nama tampilan')).toHaveValue('')
    expect(screen.getByLabelText('Connection string')).toHaveValue('')
    expect(screen.getByLabelText('Nama tampilan')).toHaveFocus()
    expect(screen.queryByText(/rahasia/)).not.toBeInTheDocument()
  })

  it('connection string selalu tersembunyi dan tidak punya tombol tampilkan', async () => {
    const { user } = openAs('admin', '/admin/connections')
    await fill(user, '', dsn)
    expect(screen.getByLabelText('Connection string')).toHaveAttribute('type', 'password')
    expect(screen.queryByRole('button', { name: 'Tampilkan password' })).not.toBeInTheDocument()
  })

  it('mendukung DBMS MySQL', async () => {
    const { user } = openAs('admin', '/admin/connections')
    await fill(user, 'toko', 'app:rahasia@tcp(db.internal:3306)/toko')
    await user.selectOptions(screen.getByLabelText('DBMS'), 'mysql')
    await user.click(screen.getByRole('button', { name: 'Simpan koneksi' }))
    expect(within(await rowOf('toko')).getByText('MySQL')).toBeInTheDocument()
  })

  it('isian kosong ditolak di browser tanpa mengirim request', async () => {
    const spy = interceptFetch('/admin/connections')
    const { user } = openAs('admin', '/admin/connections')
    await screen.findByRole('table', { name: 'Daftar koneksi database' })
    await user.click(screen.getByRole('button', { name: 'Simpan koneksi' }))
    expect(screen.getByText('Nama tampilan wajib diisi')).toBeInTheDocument()
    expect(screen.getByText('Connection string wajib diisi')).toBeInTheDocument()
    expect(screen.getByLabelText('Nama tampilan')).toHaveFocus()
    expect(spy.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(0)
  })

  it('nama yang sudah dipakai ditampilkan di bawah isiannya dan connection string tetap terisi', async () => {
    const { user } = openAs('admin', '/admin/connections')
    await fill(user, 'inventaris', dsn)
    await user.click(screen.getByRole('button', { name: 'Simpan koneksi' }))
    expect(await screen.findByText('Nama koneksi sudah dipakai')).toBeInTheDocument()
    expect(screen.getByLabelText('Nama tampilan')).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByLabelText('Connection string')).toHaveValue(dsn)
  })

  it('format connection string yang ditolak server ditampilkan pada isiannya', async () => {
    const { user } = openAs('admin', '/admin/connections')
    await fill(user, 'baru', 'bukan-url')
    await user.click(screen.getByRole('button', { name: 'Simpan koneksi' }))
    expect(await screen.findByText('DSN PostgreSQL harus berbentuk URL postgres://')).toBeInTheDocument()
    expect(screen.getByLabelText('Connection string')).toHaveFocus()
  })
})

describe('hapus koneksi', () => {
  it('menghapus koneksi setelah konfirmasi yang menjelaskan akibatnya', async () => {
    const { user } = openAs('admin', '/admin/connections')
    await user.click(within(await rowOf('pelaporan')).getByRole('button', { name: 'Hapus' }))
    const dialog = screen.getByRole('dialog', { name: 'Hapus koneksi' })
    expect(dialog).toHaveTextContent('pelaporan')
    expect(dialog).toHaveTextContent('Database aslinya tidak ikut terhapus')
    await user.click(within(dialog).getByRole('button', { name: 'Hapus koneksi' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Koneksi pelaporan dihapus.')
    expect((await table()).queryByText('pelaporan')).not.toBeInTheDocument()
    expect(getState().connections.some((connection) => connection.name === 'pelaporan')).toBe(false)
  })

  it('membatalkan konfirmasi tidak menghapus apa pun', async () => {
    const { user } = openAs('admin', '/admin/connections')
    await user.click(within(await rowOf('pelaporan')).getByRole('button', { name: 'Hapus' }))
    await user.click(screen.getByRole('button', { name: 'Batal' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(await rowOf('pelaporan')).toBeInTheDocument()
  })

  it('kegagalan hapus tampil di dalam dialog dan koneksi tetap ada', async () => {
    const { user } = openAs('admin', '/admin/connections')
    await user.click(within(await rowOf('pelaporan')).getByRole('button', { name: 'Hapus' }))
    interceptFetch('/admin/connections/', { failures: 1 })
    const dialog = screen.getByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Hapus koneksi' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('Tidak dapat terhubung ke server')
    expect(await rowOf('pelaporan')).toBeInTheDocument()
  })
})
