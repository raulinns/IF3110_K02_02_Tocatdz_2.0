// @vitest-environment jsdom
import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { handlers } from '../../mocks/handlers/index.ts'
import { ids } from '../../mocks/ids.ts'
import { getState } from '../../mocks/state.ts'
import { setupMockServer } from '../../mocks/testing.ts'
import { openAs } from '../../test/app.tsx'
import { interceptFetch, rejectRequest } from '../../test/network.ts'

setupMockServer(...handlers)

const table = async () => within(await screen.findByRole('table', { name: 'Daftar akses database' }))
const rowOf = async (group: string, database: string) => {
  const rows = (await table()).getAllByRole('row').slice(1)
  const row = rows.find((candidate) => {
    const cells = within(candidate).getAllByRole('cell')
    return cells[0].textContent?.trim() === group && cells[1].textContent?.trim() === database
  })
  if (!row) throw new Error(`baris ${group} ${database} tidak ada`)
  return row
}
const accessOf = (group: string, database: string) => getState().grants.find((grant) => grant.group_id === group && grant.database_id === database)?.access

// Opsi select baru tersedia setelah daftar group dan koneksi selesai dimuat
const choose = async (user: ReturnType<typeof openAs>['user'], group: string, database: string, access?: string) => {
  await screen.findByRole('option', { name: group })
  await user.selectOptions(screen.getByLabelText('Group'), group)
  await user.selectOptions(screen.getByLabelText('Database'), database)
  if (access) await user.selectOptions(screen.getByLabelText('Level akses'), access)
}

describe('daftar akses', () => {
  it('menampilkan semua grant dengan group, database, dan level aksesnya', async () => {
    openAs('admin', '/admin/grants')
    expect(await table()).toBeTruthy()
    expect((await table()).getAllByRole('row')).toHaveLength(1 + 6)
    const writers = await rowOf('writers', 'inventaris')
    expect(within(writers).getByText('Lihat + ubah')).toBeInTheDocument()
    expect(within(await rowOf('readers', 'arsip')).getByText('Lihat saja')).toBeInTheDocument()
  })

  it('menjelaskan bahwa akses diberikan ke group dan yang tertinggi yang berlaku', async () => {
    openAs('admin', '/admin/grants')
    expect(await screen.findByText(/akses tertinggi untuk database yang sama yang berlaku/)).toBeInTheDocument()
  })

  it('user biasa mendapat halaman akses ditolak', async () => {
    openAs('observer', '/admin/grants')
    expect(await screen.findByText(/akses ditolak/i)).toBeInTheDocument()
  })

  it('tanpa grant tampil keterangan kosong', async () => {
    getState().grants = []
    openAs('admin', '/admin/grants')
    expect(await screen.findByText('Belum ada akses yang diberikan.')).toBeInTheDocument()
  })
})

describe('beri atau ubah akses', () => {
  it('memberi akses baru dan langsung tampil di daftar', async () => {
    const { user } = openAs('admin', '/admin/grants')
    await choose(user, 'magang', 'arsip', 'view_edit')
    await user.click(screen.getByRole('button', { name: 'Simpan akses' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Akses group magang ke database arsip disimpan: Lihat + ubah.')
    expect(within(await rowOf('magang', 'arsip')).getByText('Lihat + ubah')).toBeInTheDocument()
    expect(accessOf(ids.magang, ids.arsip)).toBe('view_edit')
  })

  it('akses bawaan adalah lihat saja', async () => {
    const { user } = openAs('admin', '/admin/grants')
    await choose(user, 'magang', 'arsip')
    expect(screen.getByLabelText('Level akses')).toHaveValue('view_only')
    await user.click(screen.getByRole('button', { name: 'Simpan akses' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Lihat saja')
  })

  it('menyimpan pasangan yang sudah ada mengubah levelnya tanpa menambah baris', async () => {
    const { user } = openAs('admin', '/admin/grants')
    await choose(user, 'readers', 'inventaris', 'view_edit')
    await user.click(screen.getByRole('button', { name: 'Simpan akses' }))
    await screen.findByRole('status')
    expect(accessOf(ids.readers, ids.inventaris)).toBe('view_edit')
    expect((await table()).getAllByRole('row')).toHaveLength(1 + 6)
  })

  it('tombol Ubah mengisi form dengan nilai baris dan memindahkan fokus ke level akses', async () => {
    const { user } = openAs('admin', '/admin/grants')
    await user.click(within(await rowOf('readers', 'inventaris')).getByRole('button', { name: 'Ubah' }))
    await screen.findByRole('option', { name: 'readers' })
    expect(screen.getByLabelText('Group')).toHaveValue(ids.readers)
    expect(screen.getByLabelText('Database')).toHaveValue(ids.inventaris)
    expect(screen.getByLabelText('Level akses')).toHaveValue('view_only')
    expect(screen.getByLabelText('Level akses')).toHaveFocus()
    await user.selectOptions(screen.getByLabelText('Level akses'), 'view_edit')
    await user.click(screen.getByRole('button', { name: 'Simpan akses' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Akses group readers ke database inventaris disimpan: Lihat + ubah.')
    expect(accessOf(ids.readers, ids.inventaris)).toBe('view_edit')
  })

  it('group atau database yang belum dipilih ditolak tanpa mengirim request', async () => {
    const spy = interceptFetch('/admin/grants/')
    const { user } = openAs('admin', '/admin/grants')
    await screen.findByRole('option', { name: 'magang' })
    await user.click(screen.getByRole('button', { name: 'Simpan akses' }))
    // Teks yang sama juga ada sebagai opsi placeholder sehingga pencarian dibatasi pada pesan error
    const feedback = { selector: '.invalid-feedback' }
    expect(screen.getByText('Pilih group', feedback)).toBeInTheDocument()
    expect(screen.getByText('Pilih database', feedback)).toBeInTheDocument()
    expect(screen.getByLabelText('Group')).toHaveFocus()
    expect(screen.getByLabelText('Group')).toHaveAttribute('aria-invalid', 'true')
    await user.selectOptions(screen.getByLabelText('Group'), 'magang')
    expect(screen.queryByText('Pilih group', feedback)).not.toBeInTheDocument()
    expect(spy.mock.calls.filter(([, init]) => init?.method === 'PUT')).toHaveLength(0)
  })

  it('kegagalan jaringan ditampilkan dan pilihan tidak hilang', async () => {
    const { user } = openAs('admin', '/admin/grants')
    await choose(user, 'magang', 'arsip')
    interceptFetch('/admin/grants/', { failures: 1 })
    await user.click(screen.getByRole('button', { name: 'Simpan akses' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Tidak dapat terhubung ke server')
    expect(screen.getByLabelText('Group')).toHaveValue(ids.magang)
  })

  it('tanpa group form dikunci dengan petunjuk membuat group lebih dulu', async () => {
    getState().groups = []
    getState().grants = []
    openAs('admin', '/admin/grants')
    expect(await screen.findByText('Buat group terlebih dahulu sebelum memberi akses.')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Simpan akses' })).toBeDisabled()
  })
})

describe('cabut akses', () => {
  it('mencabut akses setelah konfirmasi yang menjelaskan akibatnya', async () => {
    const { user } = openAs('admin', '/admin/grants')
    await user.click(within(await rowOf('auditors', 'pelaporan')).getByRole('button', { name: 'Cabut' }))
    const dialog = screen.getByRole('dialog', { name: 'Cabut akses' })
    expect(dialog).toHaveTextContent('auditors')
    expect(dialog).toHaveTextContent('pelaporan')
    await user.click(within(dialog).getByRole('button', { name: 'Cabut akses' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Akses group auditors ke database pelaporan dicabut.')
    expect((await table()).getAllByRole('row')).toHaveLength(1 + 5)
    expect(accessOf(ids.auditors, ids.pelaporan)).toBeUndefined()
  })

  it('membatalkan konfirmasi tidak mencabut apa pun', async () => {
    const { user } = openAs('admin', '/admin/grants')
    await user.click(within(await rowOf('auditors', 'pelaporan')).getByRole('button', { name: 'Cabut' }))
    await user.click(screen.getByRole('button', { name: 'Batal' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(accessOf(ids.auditors, ids.pelaporan)).toBe('view_only')
  })

  it('mencabut akses yang masih ditunggu pekerjaan ditolak server dengan penjelasan', async () => {
    rejectRequest('DELETE', '/admin/grants/', 409, 'conflict')
    const { user } = openAs('admin', '/admin/grants')
    await user.click(within(await rowOf('auditors', 'pelaporan')).getByRole('button', { name: 'Cabut' }))
    const dialog = screen.getByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Cabut akses' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('Tidak dapat dihapus karena masih ada permintaan, job, atau eksekusi yang belum selesai')
    expect(accessOf(ids.auditors, ids.pelaporan)).toBe('view_only')
  })

  it('kegagalan saat mencabut tampil di dalam dialog', async () => {
    const { user } = openAs('admin', '/admin/grants')
    await user.click(within(await rowOf('auditors', 'pelaporan')).getByRole('button', { name: 'Cabut' }))
    interceptFetch('/admin/grants/', { failures: 1 })
    const dialog = screen.getByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Cabut akses' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('Tidak dapat terhubung ke server')
    expect(accessOf(ids.auditors, ids.pelaporan)).toBe('view_only')
  })

  it('mencabut akses mengubah database yang terlihat oleh anggota group', async () => {
    const { user } = openAs('admin', '/admin/grants')
    await user.click(within(await rowOf('readers', 'arsip')).getByRole('button', { name: 'Cabut' }))
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Cabut akses' }))
    await screen.findByRole('status')
    const readerGrants = getState().grants.filter((grant) => grant.group_id === ids.readers)
    expect(readerGrants.map((grant) => grant.database_id)).toEqual([ids.inventaris])
  })
})
