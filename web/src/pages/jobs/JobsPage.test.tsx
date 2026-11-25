// @vitest-environment jsdom
import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { handlers } from '../../mocks/handlers/index.ts'
import { ids, workflowIds } from '../../mocks/ids.ts'
import { getState } from '../../mocks/state.ts'
import { setupMockServer } from '../../mocks/testing.ts'
import { openAs } from '../../test/app.tsx'
import { seedBatchMigration, seedJob } from '../../test/workflow.ts'

setupMockServer(...handlers)

const list = `/databases/${ids.inventaris}/jobs`
const table = async () => within(await screen.findByRole('table', { name: 'Daftar job' }))
const bodyRows = async () => (await table()).getAllByRole('row').slice(1)

describe('daftar job', () => {
  it('menampilkan job terbaru lebih dulu dengan jenis, status, percobaan, dan durasi', async () => {
    openAs('reviewer', list)
    const rows = await bodyRows()
    expect(rows).toHaveLength(5)
    const kinds = rows.map((row) => within(row).getAllByRole('cell')[1].textContent)
    expect(kinds).toEqual(['Terapkan', 'Forward', 'Rollback', 'Terapkan', 'Terapkan'])
    expect(within(rows[0]).getByText('Berhasil')).toBeInTheDocument()
    expect(within(rows[0]).getByText('1/3')).toBeInTheDocument()
    expect(within(rows[0]).getAllByRole('cell')[5]).toHaveTextContent(/ms|dtk/)
  })

  it('judul menaut ke monitor job', async () => {
    openAs('reviewer', list)
    const row = (await bodyRows())[0]
    expect(within(row).getByRole('link', { name: 'Arsipkan order selesai' })).toHaveAttribute('href', `${list}/${workflowIds.job(5)}`)
  })

  it('filter status memakai URL', async () => {
    seedJob()
    const { user } = openAs('reviewer', list)
    await bodyRows()
    await user.selectOptions(screen.getByLabelText('Status'), 'queued')
    expect(await screen.findByText('Antre', { selector: '.badge' })).toBeInTheDocument()
    expect(await bodyRows()).toHaveLength(1)
    await user.selectOptions(screen.getByLabelText('Status'), '')
    expect(await bodyRows()).toHaveLength(6)
  })

  it('filter tanpa hasil menjelaskan status yang dicari dan status yang salah diabaikan', async () => {
    const view = openAs('reviewer', `${list}?status=failed`)
    expect(await screen.findByText('Tidak ada job dengan status gagal.')).toBeInTheDocument()
    view.unmount()
    openAs('reviewer', `${list}?status=ngawur`)
    expect(await bodyRows()).toHaveLength(5)
  })

  it('database yang ditahan job sebagian menampilkan peringatan dengan tautan ke job penahannya', async () => {
    const { job } = seedJob(seedBatchMigration("status = 'arsip' -- mock:partial"))
    job.status = 'partial'
    job.partial = true
    openAs('reviewer', list)
    const warning = await screen.findByText(/Database sedang ditahan oleh/)
    expect(within(warning.closest('.alert') as HTMLElement).getByRole('link', { name: 'sebuah job' })).toHaveAttribute('href', `${list}/${job.id}`)
  })

  it('tanpa penahan tidak ada peringatan', async () => {
    openAs('reviewer', list)
    await bodyRows()
    expect(screen.queryByText(/Database sedang ditahan oleh/)).not.toBeInTheDocument()
  })

  it('pengguna dengan akses lihat saja bisa membaca daftar', async () => {
    openAs('observer', list)
    expect(await bodyRows()).toHaveLength(5)
  })

  it('database tanpa job menampilkan keterangan kosong', async () => {
    openAs('reviewer', `/databases/${ids.pelaporan}/jobs`)
    expect(await screen.findByText('Belum ada job untuk database ini.')).toBeInTheDocument()
  })

  it('halaman di luar jangkauan menawarkan kembali ke halaman pertama', async () => {
    const { user } = openAs('reviewer', `${list}?page=9`)
    expect(await screen.findByText(/Tidak ada job di halaman ini/)).toBeInTheDocument()
    await user.click(screen.getByRole('link', { name: 'Kembali ke halaman pertama' }))
    expect(await bodyRows()).toHaveLength(5)
  })

  it('job yang belum selesai menampilkan durasi yang terus berjalan', async () => {
    const { job } = seedJob()
    job.status = 'running'
    job.started_at = new Date(Date.now() - 5000).toISOString()
    openAs('reviewer', list)
    const row = (await bodyRows())[0]
    expect(within(row).getByText('Berjalan')).toBeInTheDocument()
    expect(within(row).getAllByRole('cell')[5]).toHaveTextContent(/dtk/)
    expect(getState().jobs).toHaveLength(6)
  })
})
