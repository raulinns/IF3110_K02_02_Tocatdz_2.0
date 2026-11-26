// @vitest-environment jsdom
import { screen, within } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { handlers } from '../../mocks/handlers/index.ts'
import { getState } from '../../mocks/state.ts'
import { setupMockServer } from '../../mocks/testing.ts'
import { openAs } from '../../test/app.tsx'
import { interceptFetch } from '../../test/network.ts'

setupMockServer(...handlers)

const table = async () => within(await screen.findByRole('table', { name: 'Daftar group' }))
const rowOf = async (name: string) => (await table()).getByText(name).closest('tr') as HTMLElement

describe('daftar group', () => {
  it('menampilkan group terurut menurut nama dengan jumlah anggota dan leader', async () => {
    openAs('admin', '/admin/groups')
    const names = (await table()).getAllByRole('row').slice(1).map((row) => within(row).getAllByRole('cell')[0].textContent?.trim())
    expect(names).toEqual(['auditors', 'magang', 'readers', 'writers'])
    const writers = await rowOf('writers')
    expect(within(writers).getByText('2')).toBeInTheDocument()
    expect(within(writers).getByText('reviewer')).toBeInTheDocument()
    expect(within(await rowOf('readers')).getByText('observer')).toBeInTheDocument()
  })

  it('group tanpa leader diberi penanda peringatan', async () => {
    openAs('admin', '/admin/groups')
    const magang = await rowOf('magang')
    expect(within(magang).getByText('Belum ada leader')).toBeInTheDocument()
    expect(within(magang).getByText('0')).toBeInTheDocument()
    expect(screen.getAllByText('Belum ada leader')).toHaveLength(1)
  })

  it('user biasa mendapat halaman akses ditolak', async () => {
    openAs('editor', '/admin/groups')
    expect(await screen.findByText(/akses ditolak/i)).toBeInTheDocument()
  })

  it('tombol Kelola membuka halaman detail group', async () => {
    const { user } = openAs('admin', '/admin/groups')
    await user.click(within(await rowOf('writers')).getByRole('link', { name: 'Kelola group writers' }))
    expect(await screen.findByRole('table', { name: 'Anggota group writers' })).toBeInTheDocument()
  })

  it('halaman di luar jangkauan menawarkan kembali ke halaman pertama', async () => {
    const { user } = openAs('admin', '/admin/groups?page=9')
    expect(await screen.findByText(/Tidak ada group di halaman ini/)).toBeInTheDocument()
    await user.click(screen.getByRole('link', { name: 'Kembali ke halaman pertama' }))
    expect(await rowOf('writers')).toBeInTheDocument()
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

describe('buat group', () => {
  it('membuat group dengan nama dipangkas lalu form dikosongkan', async () => {
    const { user } = openAs('admin', '/admin/groups')
    await screen.findByRole('table', { name: 'Daftar group' })
    await user.type(screen.getByLabelText('Nama group'), '  Tim Baru  ')
    await user.click(screen.getByRole('button', { name: 'Buat group' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Group Tim Baru berhasil dibuat.')
    const baru = await rowOf('Tim Baru')
    expect(within(baru).getByText('Belum ada leader')).toBeInTheDocument()
    expect(screen.getByLabelText('Nama group')).toHaveValue('')
    expect(screen.getByLabelText('Nama group')).toHaveFocus()
  })

  it('nama kosong ditolak di browser tanpa mengirim request', async () => {
    const spy = interceptFetch('/admin/groups')
    const { user } = openAs('admin', '/admin/groups')
    await screen.findByRole('table', { name: 'Daftar group' })
    await user.type(screen.getByLabelText('Nama group'), '   ')
    await user.click(screen.getByRole('button', { name: 'Buat group' }))
    expect(screen.getByText('Nama group wajib diisi')).toBeInTheDocument()
    expect(screen.getByLabelText('Nama group')).toHaveFocus()
    expect(spy.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(0)
  })

  it('nama terlalu panjang ditolak', async () => {
    const { user } = openAs('admin', '/admin/groups')
    await screen.findByRole('table', { name: 'Daftar group' })
    await user.type(screen.getByLabelText('Nama group'), 'g'.repeat(65))
    await user.click(screen.getByRole('button', { name: 'Buat group' }))
    expect(screen.getByText('Nama group maksimal 64 karakter')).toBeInTheDocument()
  })

  it('nama yang sudah dipakai ditampilkan di bawah isiannya tanpa membedakan huruf besar', async () => {
    const { user } = openAs('admin', '/admin/groups')
    await screen.findByRole('table', { name: 'Daftar group' })
    await user.type(screen.getByLabelText('Nama group'), 'WRITERS')
    await user.click(screen.getByRole('button', { name: 'Buat group' }))
    expect(await screen.findByText('Nama group sudah dipakai')).toBeInTheDocument()
    expect(screen.getByLabelText('Nama group')).toHaveAttribute('aria-invalid', 'true')
    expect(screen.getByLabelText('Nama group')).toHaveValue('WRITERS')
    expect(getState().groups).toHaveLength(4)
  })

  it('konflik dengan pesan server yang umum tetap ditampilkan sebagai nama group sudah dipakai', async () => {
    genericConflict('/admin/groups')
    const { user } = openAs('admin', '/admin/groups')
    await screen.findByRole('table', { name: 'Daftar group' })
    await user.type(screen.getByLabelText('Nama group'), 'tim-baru')
    await user.click(screen.getByRole('button', { name: 'Buat group' }))
    expect(await screen.findByText('Nama group sudah dipakai')).toBeInTheDocument()
    expect(screen.queryByText(/Data atau status berubah/)).not.toBeInTheDocument()
  })

  it('kegagalan jaringan ditampilkan sebagai notifikasi dan isian tetap ada', async () => {
    const { user } = openAs('admin', '/admin/groups')
    await screen.findByRole('table', { name: 'Daftar group' })
    await user.type(screen.getByLabelText('Nama group'), 'tim-baru')
    interceptFetch('/admin/groups', { failures: 1 })
    await user.click(screen.getByRole('button', { name: 'Buat group' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Tidak dapat terhubung ke server')
    expect(screen.getByLabelText('Nama group')).toHaveValue('tim-baru')
  })
})
