// @vitest-environment jsdom
import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { handlers } from '../../mocks/handlers/index.ts'
import { ids, workflowIds } from '../../mocks/ids.ts'
import { getState } from '../../mocks/state.ts'
import { setupMockServer } from '../../mocks/testing.ts'
import { openAs } from '../../test/app.tsx'

setupMockServer(...handlers)

const list = `/databases/${ids.inventaris}/migrations`
const table = async () => within(await screen.findByRole('table', { name: 'Daftar migration' }))
const rowOf = async (title: string) => (await table()).getByText(title).closest('tr') as HTMLElement

describe('daftar migration', () => {
  it('menampilkan migration terbaru lebih dulu dengan mode, status, dan revision', async () => {
    openAs('reviewer', list)
    const titles = (await table()).getAllByRole('row').slice(1).map((row) => within(row).getAllByRole('cell')[0].textContent?.replace('Irreversible', '').trim())
    expect(titles).toEqual(['Hapus kolom promo lama', 'Tambah indeks status order', 'Arsipkan order selesai', 'Tambah kolom diskon pada promo', 'Buat tabel promo'])
    const batch = await rowOf('Arsipkan order selesai')
    expect(within(batch).getByText('Batch')).toBeInTheDocument()
    expect(within(batch).getByText('Dibekukan')).toBeInTheDocument()
    expect(within(batch).getByText('Pernah dijalankan')).toBeInTheDocument()
    const draft = await rowOf('Tambah indeks status order')
    expect(within(draft).getByText('Draft')).toBeInTheDocument()
    expect(within(draft).getByText('Normal')).toBeInTheDocument()
    expect(within(draft).queryByText('Pernah dijalankan')).not.toBeInTheDocument()
  })

  it('migration irreversible diberi penanda', async () => {
    openAs('reviewer', list)
    expect(within(await rowOf('Hapus kolom promo lama')).getByText('Irreversible')).toBeInTheDocument()
    expect(within(await rowOf('Buat tabel promo')).queryByText('Irreversible')).not.toBeInTheDocument()
  })

  it('judul menaut ke detail migration', async () => {
    openAs('reviewer', list)
    expect(within(await rowOf('Tambah indeks status order')).getByRole('link', { name: 'Tambah indeks status order' })).toHaveAttribute('href', `${list}/${workflowIds.migrationD}`)
  })

  it('tombol Buat migration hanya untuk yang boleh mengubah database', async () => {
    const editor = openAs('reviewer', list)
    expect(await screen.findByRole('link', { name: 'Buat migration' })).toHaveAttribute('href', `${list}/new`)
    editor.unmount()
    openAs('observer', list)
    await table()
    expect(screen.queryByRole('link', { name: 'Buat migration' })).not.toBeInTheDocument()
  })

  it('tanpa migration tampil keterangan kosong', async () => {
    getState().migrations = []
    openAs('reviewer', list)
    expect(await screen.findByText('Belum ada migration untuk database ini.')).toBeInTheDocument()
  })

  it('halaman di luar jangkauan menawarkan kembali ke halaman pertama', async () => {
    const { user } = openAs('reviewer', `${list}?page=9`)
    expect(await screen.findByText(/Tidak ada migration di halaman ini/)).toBeInTheDocument()
    await user.click(screen.getByRole('link', { name: 'Kembali ke halaman pertama' }))
    expect(await rowOf('Buat tabel promo')).toBeInTheDocument()
  })

  it('hanya menampilkan migration milik database yang dibuka', async () => {
    openAs('reviewer', `/databases/${ids.pelaporan}/migrations`)
    expect(await rowOf('Tambah tabel ringkasan bulanan')).toBeInTheDocument()
    expect(screen.queryByText('Buat tabel promo')).not.toBeInTheDocument()
  })

  it('tab Migration tersedia di database PostgreSQL dan tidak di MySQL', async () => {
    openAs('observer', `/databases/${ids.arsip}/schema`)
    await screen.findByLabelText('Bagian database')
    expect(within(screen.getByLabelText('Bagian database')).queryByRole('link', { name: 'Migration' })).not.toBeInTheDocument()
  })
})
