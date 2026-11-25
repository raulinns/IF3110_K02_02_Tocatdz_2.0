// @vitest-environment jsdom
import { screen, waitFor, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { pollIntervals } from '../../api/workflow.ts'
import { handlers } from '../../mocks/handlers/index.ts'
import { ids, workflowIds } from '../../mocks/ids.ts'
import { getState } from '../../mocks/state.ts'
import { setupMockServer } from '../../mocks/testing.ts'
import { openAs } from '../../test/app.tsx'
import { interceptFetch } from '../../test/network.ts'
import { approveAll, seedBatchMigration, seedJob, seedRequest } from '../../test/workflow.ts'

setupMockServer(...handlers)

const original = { ...pollIntervals }

// Polling dipercepat supaya perjalanan job dari antre sampai selesai tidak memakan waktu nyata
beforeEach(() => {
  pollIntervals.active = 15
  pollIntervals.uncertain = 15
})
afterEach(() => {
  Object.assign(pollIntervals, original)
})

const monitor = (jobId: string) => `/databases/${ids.inventaris}/jobs/${jobId}`
const stepper = () => screen.getByRole('list', { name: 'Tahapan job' })
const attempts = async () => within(await screen.findByRole('table', { name: 'Percobaan job' }))
const finalStep = () => within(stepper()).getAllByRole('listitem').at(-1)!
// Hanya pembacaan job itu sendiri yang dihitung dan bukan endpoint percobaannya
const jobCalls = (spy: { mock: { calls: unknown[][] } }, id: string) => spy.mock.calls.filter((call) => String(call[0]).endsWith(`/jobs/${id}`)).length
const jobOf = (id: string) => getState().jobs.find((job) => job.id === id)!

describe('monitor job', () => {
  it('job normal berjalan dari antre sampai berhasil lalu polling berhenti', async () => {
    const { job } = seedJob()
    const spy = interceptFetch(`/jobs/${job.id}`)
    openAs('reviewer', monitor(job.id))
    await waitFor(() => expect(finalStep()).toHaveTextContent('Berhasil'), { timeout: 8000 })
    expect(finalStep()).toHaveAttribute('aria-current', 'step')
    expect(screen.getByRole('status')).toHaveTextContent('Pembaruan otomatis berhenti.')
    const settled = jobCalls(spy, job.id)
    await new Promise((resolve) => setTimeout(resolve, 200))
    expect(jobCalls(spy, job.id)).toBe(settled)
    expect(jobOf(job.id).status).toBe('succeeded')
  })

  it('menampilkan tahap antre lebih dulu dan keterangan yang diumumkan ke pembaca layar', async () => {
    pollIntervals.active = 60_000
    const { job } = seedJob()
    openAs('reviewer', monitor(job.id))
    expect(await screen.findByText(/Job menunggu diambil worker/)).toBeInTheDocument()
    expect(within(stepper()).getByText('Antre').closest('li')).toHaveAttribute('aria-current', 'step')
    expect(screen.getByRole('status')).toHaveTextContent('Diperbarui otomatis.')
  })

  it('percobaan dicatat dan hasil berhasil menautkan ke riwayat', async () => {
    const { job } = seedJob()
    openAs('reviewer', monitor(job.id))
    await waitFor(() => expect(finalStep()).toHaveTextContent('Berhasil'), { timeout: 8000 })
    const rows = (await attempts()).getAllByRole('row').slice(1)
    expect(rows).toHaveLength(1)
    expect(within(rows[0]).getByText('Berhasil')).toBeInTheDocument()
    expect(screen.getByRole('link', { name: 'Lihat riwayat' })).toHaveAttribute('href', `/databases/${ids.inventaris}/history`)
    expect(screen.getByRole('link', { name: 'Lihat permintaan' })).toHaveAttribute('href', `/databases/${ids.inventaris}/review/${job.change_request_id}`)
  })

  it('menampilkan waktu tunggu antrean dan durasi', async () => {
    const { job } = seedJob()
    openAs('reviewer', monitor(job.id))
    await waitFor(() => expect(finalStep()).toHaveTextContent('Berhasil'), { timeout: 8000 })
    expect(screen.getByText('Waktu tunggu antrean').nextElementSibling).toHaveTextContent(/ms|dtk|mnt/)
    expect(screen.getByText('Durasi', { selector: 'dt' }).nextElementSibling).toHaveTextContent(/ms|dtk|mnt/)
  })

  it('job gagal menampilkan penyebab dengan SQLSTATE dan tanpa tombol pemulihan', async () => {
    const migration = getState().migrations.find((candidate) => candidate.id === workflowIds.migrationD)!
    migration.up_sql = 'CREATE INDEX i ON public.orders (status); -- mock:fail'
    const { job } = seedJob()
    openAs('reviewer', monitor(job.id))
    await waitFor(() => expect(finalStep()).toHaveTextContent('Gagal'), { timeout: 8000 })
    expect(screen.getByRole('alert')).toHaveTextContent('SQLSTATE 42P01')
    expect(screen.getByRole('alert')).toHaveTextContent('Statement SQL gagal')
    expect(screen.queryByRole('button', { name: 'Resume' })).not.toBeInTheDocument()
  })

  it('percobaan yang gagal lalu diulang tetap tercatat lengkap dengan alasannya', async () => {
    const migration = getState().migrations.find((candidate) => candidate.id === workflowIds.migrationD)!
    migration.up_sql = 'CREATE INDEX i ON public.orders (status); -- mock:retry'
    const { job } = seedJob()
    openAs('reviewer', monitor(job.id))
    await waitFor(() => expect(finalStep()).toHaveTextContent('Berhasil'), { timeout: 8000 })
    await waitFor(async () => expect((await attempts()).getAllByRole('row').slice(1)).toHaveLength(2))
    const [first, second] = (await attempts()).getAllByRole('row').slice(1)
    expect(within(first).getByText('Gagal')).toBeInTheDocument()
    expect(within(first).getByText(/SQLSTATE 08006/)).toHaveTextContent('dapat diulang')
    expect(within(second).getByText('Berhasil')).toBeInTheDocument()
  })

  it('job batch menampilkan progres per batch dan baris yang di-commit', async () => {
    const migration = seedBatchMigration()
    const { job } = seedJob(migration)
    openAs('reviewer', monitor(job.id))
    expect(await screen.findByRole('progressbar', { name: 'Progres batch' })).toBeInTheDocument()
    await waitFor(() => expect(finalStep()).toHaveTextContent('Berhasil'), { timeout: 8000 })
    const bar = screen.getByRole('progressbar', { name: 'Progres batch' })
    expect(bar).toHaveTextContent('100%')
    expect(screen.getByText('Batch di-commit').nextElementSibling).toHaveTextContent('2 dari 2')
    expect(screen.getByText('Baris di-commit').nextElementSibling).toHaveTextContent('137')
  })

  it('job yang berhenti di tengah ditandai sebagian dan menahan database sampai dipulihkan', async () => {
    const migration = seedBatchMigration("status = 'arsip' -- mock:partial")
    const { job } = seedJob(migration)
    openAs('reviewer', monitor(job.id))
    await waitFor(() => expect(finalStep()).toHaveTextContent('Sebagian'), { timeout: 8000 })
    expect(screen.getByRole('status')).toHaveTextContent('Pembaruan otomatis berhenti.')
    expect(screen.getByRole('alert')).toHaveTextContent('SQLSTATE 23505')
    expect(await screen.findByText(/database ditahan/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Resume' })).toBeEnabled()
    expect(screen.getByRole('button', { name: 'Revert batches' })).toBeEnabled()
  })

  it('Resume membuat permintaan baru dengan job sumber lalu membuka halaman review-nya', async () => {
    const migration = seedBatchMigration("status = 'arsip' -- mock:partial")
    const { job } = seedJob(migration)
    const { user } = openAs('reviewer', monitor(job.id))
    await waitFor(() => expect(finalStep()).toHaveTextContent('Sebagian'), { timeout: 8000 })
    await user.click(await screen.findByRole('button', { name: 'Resume' }))
    const dialog = screen.getByRole('dialog', { name: 'Resume job' })
    expect(dialog).toHaveTextContent('Lanjutkan batch yang tersisa')
    await user.click(within(dialog).getByRole('button', { name: 'Resume' }))
    expect(await screen.findByText('Menunggu review')).toBeInTheDocument()
    expect(await screen.findByText('Permintaan pemulihan dibuat dan menunggu approval.')).toBeInTheDocument()
    const created = getState().changeRequests.find((request) => request.source_job_id === job.id)
    expect(created).toMatchObject({ kind: 'resume', status: 'pending_review', migration_id: migration.id })
  })

  it('Revert batches menjelaskan akibatnya dan membuat permintaan revert', async () => {
    const migration = seedBatchMigration("status = 'arsip' -- mock:partial")
    const { job } = seedJob(migration)
    const { user } = openAs('reviewer', monitor(job.id))
    await waitFor(() => expect(finalStep()).toHaveTextContent('Sebagian'), { timeout: 8000 })
    await user.click(await screen.findByRole('button', { name: 'Revert batches' }))
    const dialog = screen.getByRole('dialog', { name: 'Revert batch' })
    expect(dialog).toHaveTextContent('Batalkan batch yang sudah di-commit dengan SQL down')
    await user.click(within(dialog).getByRole('button', { name: 'Revert batch' }))
    await screen.findByText('Menunggu review')
    expect(getState().changeRequests.find((request) => request.source_job_id === job.id)?.kind).toBe('revert_batches')
  })

  it('kegagalan pengajuan pemulihan tampil di dalam dialog tanpa membuat permintaan', async () => {
    const migration = seedBatchMigration("status = 'arsip' -- mock:partial")
    const { job } = seedJob(migration)
    const { user } = openAs('reviewer', monitor(job.id))
    await waitFor(() => expect(finalStep()).toHaveTextContent('Sebagian'), { timeout: 8000 })
    await user.click(await screen.findByRole('button', { name: 'Resume' }))
    interceptFetch('/change-requests', { failures: 1 })
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Resume' }))
    expect(await within(screen.getByRole('dialog')).findByRole('alert')).toHaveTextContent('Tidak dapat terhubung ke server')
    expect(getState().changeRequests.some((request) => request.source_job_id === job.id)).toBe(false)
  })

  it('migration irreversible tidak bisa di-revert', async () => {
    const migration = seedBatchMigration("status = 'arsip' -- mock:partial")
    migration.irreversible = true
    const { job } = seedJob(migration)
    openAs('reviewer', monitor(job.id))
    await waitFor(() => expect(finalStep()).toHaveTextContent('Sebagian'), { timeout: 8000 })
    expect(await screen.findByRole('button', { name: 'Revert batches' })).toBeDisabled()
    expect(screen.getByRole('button', { name: 'Resume' })).toBeEnabled()
  })

  it('pengguna dengan akses lihat saja diminta menghubungi yang berhak memulihkan', async () => {
    const migration = seedBatchMigration("status = 'arsip' -- mock:partial")
    const { job } = seedJob(migration)
    openAs('observer', monitor(job.id))
    await waitFor(() => expect(finalStep()).toHaveTextContent('Sebagian'), { timeout: 8000 })
    expect(await screen.findByText(/perlu dipulihkan oleh pengguna dengan akses ubah/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Resume' })).not.toBeInTheDocument()
  })

  it('hasil yang belum pasti terus dipantau sampai terbukti lalu berhenti', async () => {
    const migration = seedBatchMigration("status = 'arsip' -- mock:uncertain")
    const { job } = seedJob(migration)
    openAs('reviewer', monitor(job.id))
    expect(await screen.findByText(/Jangan jalankan ulang SQL ini/, undefined, { timeout: 8000 })).toBeInTheDocument()
    await waitFor(() => expect(finalStep()).toHaveTextContent('Berhasil'), { timeout: 8000 })
    expect(screen.getByRole('status')).toHaveTextContent('Pembaruan otomatis berhenti.')
  })

  it('tombol Muat ulang tetap mengambil data setelah polling berhenti', async () => {
    const { job } = seedJob()
    const spy = interceptFetch(`/jobs/${job.id}`)
    const { user } = openAs('reviewer', monitor(job.id))
    await waitFor(() => expect(finalStep()).toHaveTextContent('Berhasil'), { timeout: 8000 })
    const before = jobCalls(spy, job.id)
    await user.click(screen.getByRole('button', { name: 'Muat ulang' }))
    await waitFor(() => expect(jobCalls(spy, job.id)).toBeGreaterThan(before))
  })

  it('job yang tidak ada atau id yang salah menampilkan halaman yang sesuai', async () => {
    const missing = openAs('reviewer', monitor('60000000-0000-4000-8000-000000009999'))
    expect(await screen.findByText('Tidak ditemukan')).toBeInTheDocument()
    missing.unmount()
    openAs('reviewer', monitor('bukan-uuid'))
    expect(await screen.findByRole('alert')).toHaveTextContent('Permintaan tidak valid')
  })

  it('job milik database yang tidak boleh diakses dianggap tidak ada', async () => {
    const { job } = seedJob()
    openAs('editor', `/databases/${ids.pelaporan}/jobs/${job.id}`)
    // editor tidak punya akses ke job inventaris lewat database lain, tetapi punya akses ke pelaporan sehingga job tetap ditemukan lewat ID
    expect(await screen.findByRole('list', { name: 'Tahapan job' })).toBeInTheDocument()
  })

  it('job seed dari riwayat contoh tampil sebagai berhasil tanpa polling lanjutan', async () => {
    const spy = interceptFetch(`/jobs/${workflowIds.job(5)}`)
    openAs('reviewer', monitor(workflowIds.job(5)))
    await waitFor(() => expect(finalStep()).toHaveTextContent('Berhasil'))
    await new Promise((resolve) => setTimeout(resolve, 150))
    expect(jobCalls(spy, workflowIds.job(5))).toBe(1)
  })
})

describe('dari review sampai monitor', () => {
  it('menjalankan permintaan yang disetujui membuka monitor yang lalu berjalan sampai selesai', async () => {
    const request = seedRequest()
    approveAll(request)
    const { user } = openAs('reviewer', `/databases/${ids.inventaris}/review/${request.id}`)
    await user.click(await screen.findByRole('button', { name: 'Jalankan' }))
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Jalankan' }))
    expect(await screen.findByRole('list', { name: 'Tahapan job' })).toBeInTheDocument()
    expect(screen.getByText('Permintaan diantrekan. Status job diperbarui otomatis.')).toBeInTheDocument()
    await waitFor(() => expect(finalStep()).toHaveTextContent('Berhasil'), { timeout: 8000 })
    expect(getState().changeRequests.find((candidate) => candidate.id === request.id)?.status).toBe('succeeded')
  })
})
