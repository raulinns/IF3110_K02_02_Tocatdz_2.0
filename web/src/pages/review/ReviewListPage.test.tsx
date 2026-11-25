// @vitest-environment jsdom
import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { handlers } from '../../mocks/handlers/index.ts'
import { ids, workflowIds } from '../../mocks/ids.ts'
import { setupMockServer } from '../../mocks/testing.ts'
import { openAs } from '../../test/app.tsx'
import { approveAll, seedRequest } from '../../test/workflow.ts'

setupMockServer(...handlers)

const list = `/databases/${ids.inventaris}/review`
const table = async () => within(await screen.findByRole('table', { name: 'Daftar permintaan perubahan' }))
const bodyRows = async () => (await table()).getAllByRole('row').slice(1)

describe('daftar permintaan', () => {
  it('menampilkan permintaan terbaru lebih dulu dengan judul, jenis, status, dan jumlah approval', async () => {
    openAs('reviewer', list)
    const rows = await bodyRows()
    expect(rows).toHaveLength(5)
    const titles = rows.map((row) => within(row).getAllByRole('cell')[0].textContent)
    expect(titles).toEqual(['Arsipkan order selesai', 'Tambah kolom diskon pada promo', 'Tambah kolom diskon pada promo', 'Tambah kolom diskon pada promo', 'Buat tabel promo'])
    const kinds = rows.map((row) => within(row).getAllByRole('cell')[1].textContent)
    expect(kinds).toEqual(['Terapkan', 'Forward', 'Rollback', 'Terapkan', 'Terapkan'])
    expect(within(rows[0]).getByText('Berhasil')).toBeInTheDocument()
    expect(within(rows[0]).getByText('2/2 group')).toBeInTheDocument()
  })

  it('permintaan yang menunggu keputusan leader diberi penanda hanya untuk leader itu', async () => {
    seedRequest({ status: 'pending_review' })
    const view = openAs('reviewer', list)
    expect(await screen.findByText('Menunggu keputusan Anda')).toBeInTheDocument()
    view.unmount()
    openAs('editor', list)
    await bodyRows()
    expect(screen.queryByText('Menunggu keputusan Anda')).not.toBeInTheDocument()
  })

  it('leader yang sudah memutuskan tidak lagi diberi penanda', async () => {
    const request = seedRequest()
    Object.assign(request.approvals[0], { decision: 'approve', actor_id: ids.reviewer })
    openAs('reviewer', list)
    await bodyRows()
    expect(screen.queryByText('Menunggu keputusan Anda')).not.toBeInTheDocument()
  })

  it('judul menaut ke detail permintaan', async () => {
    const request = seedRequest()
    openAs('reviewer', list)
    const row = (await bodyRows())[0]
    expect(within(row).getByRole('link', { name: 'Tambah indeks status order' })).toHaveAttribute('href', `${list}/${request.id}`)
  })

  it('filter status memakai URL dan mengganti filter kembali ke halaman pertama', async () => {
    seedRequest({ status: 'pending_review' })
    const { user } = openAs('reviewer', list)
    await bodyRows()
    await user.selectOptions(screen.getByLabelText('Status'), 'pending_review')
    expect(await screen.findByText('Tambah indeks status order')).toBeInTheDocument()
    expect(await bodyRows()).toHaveLength(1)
    await user.selectOptions(screen.getByLabelText('Status'), '')
    expect(await bodyRows()).toHaveLength(6)
  })

  it('filter tanpa hasil menjelaskan status yang dicari', async () => {
    openAs('reviewer', `${list}?status=rejected`)
    expect(await screen.findByText('Tidak ada permintaan dengan status ditolak.')).toBeInTheDocument()
  })

  it('status di URL yang tidak dikenal diabaikan', async () => {
    openAs('reviewer', `${list}?status=ngawur`)
    expect(await bodyRows()).toHaveLength(5)
    expect(screen.getByLabelText('Status')).toHaveValue('')
  })

  it('pengguna dengan akses lihat saja bisa membaca daftar', async () => {
    openAs('observer', list)
    expect(await bodyRows()).toHaveLength(5)
  })

  it('halaman di luar jangkauan menawarkan kembali ke halaman pertama', async () => {
    const { user } = openAs('reviewer', `${list}?page=9`)
    expect(await screen.findByText(/Tidak ada permintaan di halaman ini/)).toBeInTheDocument()
    await user.click(screen.getByRole('link', { name: 'Kembali ke halaman pertama' }))
    expect(await bodyRows()).toHaveLength(5)
  })

  it('tanpa permintaan tampil keterangan kosong', async () => {
    openAs('reviewer', `/databases/${ids.pelaporan}/review`)
    expect(await screen.findByText('Belum ada permintaan perubahan untuk database ini.')).toBeInTheDocument()
  })

  it('permintaan yang sudah disetujui semua group ditampilkan sebagai disetujui', async () => {
    approveAll(seedRequest())
    openAs('reviewer', list)
    expect(within((await bodyRows())[0]).getByText('Disetujui')).toBeInTheDocument()
  })

  it('migration dari database lain memakai ID singkat sebagai cadangan judul', async () => {
    seedRequest({ migration_id: workflowIds.migrationP })
    openAs('reviewer', list)
    expect(await screen.findByText(/Migration 20000000/)).toBeInTheDocument()
  })
})
