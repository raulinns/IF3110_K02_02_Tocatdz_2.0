// @vitest-environment jsdom
import { screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { handlers } from '../../mocks/handlers/index.ts'
import { ids, workflowIds } from '../../mocks/ids.ts'
import { getState } from '../../mocks/state.ts'
import { setupMockServer } from '../../mocks/testing.ts'
import { openAs } from '../../test/app.tsx'
import { sqlOf } from '../../test/editor.ts'
import { interceptFetch } from '../../test/network.ts'
import { seedJob } from '../../test/workflow.ts'

setupMockServer(...handlers)

const history = `/databases/${ids.inventaris}/history`
const table = async () => within(await screen.findByRole('table', { name: 'Riwayat eksekusi' }))
const bodyRows = async () => (await table()).getAllByRole('row').slice(1).filter((row) => row.id.startsWith('execution-'))
const positions = async () => (await bodyRows()).map((row) => within(row).getAllByRole('cell')[0].textContent)

function seedFailedAttempt() {
  const { job, request } = seedJob()
  request.status = 'failed'
  job.status = 'failed'
  job.attempt = 1
  job.started_at = '2026-11-27T09:00:00Z'
  job.finished_at = '2026-11-27T09:00:01Z'
  job.error = {code: 'sql_error', message: 'Transaksi dibatalkan.', sqlstate: '23505', retryable: false}
  job.attempts = [{attempt: 1, status: 'failed', started_at: job.started_at, finished_at: job.finished_at, error: job.error}]
  return job
}

describe('riwayat seluruh attempt', () => {
  it('attempt gagal tetap terlihat saat Hide rollback aktif dan tidak mempunyai posisi commit', async () => {
    seedFailedAttempt()
    const {user} = openAs('reviewer', history)
    const badge = await screen.findByText('Gagal', {selector: '.badge'})
    const row = badge.closest('tr')!
    expect(within(row).getAllByRole('cell')[0]).toHaveTextContent('—')
    expect(within(row).queryByText('Aktif')).not.toBeInTheDocument()
    expect(within(row).getByText('Percobaan 1')).toBeInTheDocument()
    await user.click(screen.getByLabelText('Sembunyikan rollback'))
    await waitFor(async () => expect(await bodyRows()).toHaveLength(3))
    expect(screen.getByText('Gagal', {selector: '.badge'})).toBeInTheDocument()
  })

  it('detail kegagalan menampilkan snapshot dan hasil statement yang disamarkan', async () => {
    const job = seedFailedAttempt()
    const {user} = openAs('reviewer', history)
    const row = (await screen.findByText('Gagal', {selector: '.badge'})).closest('tr')!
    await user.click(within(row).getByRole('button', {name: 'SQL'}))
    expect(await sqlOf('Salinan SQL up')).toBe(job.plan.sql)
    expect(await screen.findByText(/Statement gagal.*23505/)).toBeInTheDocument()
  })

  it('pengajuan ulang membuat CR baru dan mengosongkan seluruh approval', async () => {
    const job = seedFailedAttempt()
    const {user} = openAs('reviewer', history)
    await user.click(await screen.findByRole('button', {name: 'Jalankan ulang'}))
    const dialog = screen.getByRole('dialog', {name: 'Ajukan jalankan ulang'})
    await user.click(within(dialog).getByRole('button', {name: 'Ajukan'}))
    await waitFor(() => expect(screen.getByText('Permintaan jalankan ulang dibuat. Semua group harus menyetujui lagi.')).toBeInTheDocument())
    const request = getState().changeRequests.find((entry) => entry.retry_of_job_id === job.id)!
    expect(request.status).toBe('pending_review')
    expect(request.approvals).toHaveLength(2)
    expect(request.approvals.every((approval) => approval.decision === null)).toBe(true)
    expect(getState().jobs.some((entry) => entry.change_request_id === request.id)).toBe(false)
  })

  it('pengamat dapat membaca kegagalan tanpa tombol pengajuan ulang', async () => {
    seedFailedAttempt()
    openAs('observer', history)
    await screen.findByText('Gagal', {selector: '.badge'})
    expect(screen.queryByRole('button', {name: 'Jalankan ulang'})).not.toBeInTheDocument()
  })
})

describe('riwayat eksekusi', () => {
  it('menampilkan contoh E1 sampai E5 terbaru lebih dulu dengan operasi dan arahnya', async () => {
    openAs('reviewer', history)
    expect(await positions()).toEqual(['#5', '#4', '#3', '#2', '#1'])
    const rows = await bodyRows()
    expect(rows.map((row) => within(row).getAllByRole('cell')[3].textContent)).toEqual(['Terapkan up', 'Forward up', 'Rollback down', 'Terapkan up', 'Terapkan up'])
    expect(within(rows[0]).getByText('editor')).toBeInTheDocument()
  })

  it('menandai eksekusi yang menjadi posisi aktif dan hanya satu', async () => {
    openAs('reviewer', history)
    const rows = await bodyRows()
    expect(within(rows[0]).getByText('Aktif')).toBeInTheDocument()
    expect(screen.getAllByText('Aktif', { selector: '.badge' })).toHaveLength(1)
    expect(rows[0]).toHaveAttribute('aria-current', 'true')
  })

  it('rollback menunjuk eksekusi yang dibatalkan dan forward menunjuk eksekusi asalnya', async () => {
    openAs('reviewer', history)
    const rows = await bodyRows()
    expect(within(rows[2]).getByRole('link', { name: /membatalkan #2/ })).toHaveAttribute('href', `#execution-${workflowIds.execution(2)}`)
    expect(within(rows[1]).getByRole('link', { name: /forward dari #2/ })).toBeInTheDocument()
    expect(within(rows[0]).getByText('-')).toBeInTheDocument()
  })

  it('menekan tautan terkait menuju baris tujuannya dan menandainya sebentar', async () => {
    const { user } = openAs('reviewer', history)
    const rows = await bodyRows()
    await user.click(within(rows[2]).getByRole('link', { name: /membatalkan #2/ }))
    const target = document.getElementById(`execution-${workflowIds.execution(2)}`) as HTMLElement
    expect(target).toHaveFocus()
    expect(target).toHaveClass('table-active')
    await waitFor(() => expect(target).not.toHaveClass('table-active'), { timeout: 4000 })
  })

  it('judul migration dan aktor dari server ditampilkan di tiap baris', async () => {
    openAs('observer', history)
    const rows = await bodyRows()
    expect(within(rows[0]).getByText('Arsipkan order selesai')).toBeInTheDocument()
    expect(within(rows[4]).getByText('Buat tabel promo')).toBeInTheDocument()
  })

  it('database tanpa eksekusi menampilkan keterangan kosong', async () => {
    openAs('reviewer', `/databases/${ids.pelaporan}/history`)
    expect(await screen.findByText('Belum ada eksekusi untuk database ini.')).toBeInTheDocument()
  })

  it('halaman di luar jangkauan menawarkan kembali ke halaman pertama', async () => {
    const { user } = openAs('reviewer', `${history}?page=9`)
    expect(await screen.findByText(/Tidak ada eksekusi di halaman ini/)).toBeInTheDocument()
    await user.click(screen.getByRole('link', { name: 'Kembali ke halaman pertama' }))
    expect(await bodyRows()).toHaveLength(5)
  })
})

describe('sembunyikan rollback', () => {
  it('menyembunyikan eksekusi yang di-rollback beserta rollback-nya sesuai contoh kontrak', async () => {
    const { user } = openAs('reviewer', history)
    await bodyRows()
    expect(screen.getByLabelText('Sembunyikan rollback')).not.toBeChecked()
    await user.click(screen.getByLabelText('Sembunyikan rollback'))
    await waitFor(async () => expect(await bodyRows()).toHaveLength(3))
    const visible = (await bodyRows()).map((row) => row.id.replace('execution-', ''))
    // Kecocokan E2 dan E3 dengan contoh kontrak dijaga oleh test mock berbasis fixture, dan di sini yang diuji adalah tampilannya
    const hidden = [workflowIds.execution(2), workflowIds.execution(3)]
    for (const id of hidden) expect(visible).not.toContain(id)
    expect(visible).toEqual([workflowIds.execution(5), workflowIds.execution(4), workflowIds.execution(1)])
  })

  it('pilihan tersimpan di URL dan dibaca saat halaman dibuka dengan tautan itu', async () => {
    openAs('reviewer', `${history}?hide_rollback=true`)
    expect(await screen.findByLabelText('Sembunyikan rollback')).toBeChecked()
    expect(await bodyRows()).toHaveLength(3)
  })

  it('menonaktifkan kembali menampilkan seluruh riwayat dan mengirim parameter ke server hanya saat aktif', async () => {
    const spy = interceptFetch('/history')
    const { user } = openAs('reviewer', `${history}?hide_rollback=true`)
    await bodyRows()
    await user.click(screen.getByLabelText('Sembunyikan rollback'))
    await waitFor(async () => expect(await bodyRows()).toHaveLength(5))
    const urls = spy.mock.calls.map((call) => String(call[0])).filter((url) => url.includes('/history?'))
    expect(urls.some((url) => url.includes('hide_rollback=true'))).toBe(true)
    expect(urls.at(-1)).not.toContain('hide_rollback')
  })

  it('nilai di URL selain true dianggap mati', async () => {
    openAs('reviewer', `${history}?hide_rollback=ya`)
    expect(await bodyRows()).toHaveLength(5)
    expect(screen.getByLabelText('Sembunyikan rollback')).not.toBeChecked()
  })
})

describe('salinan SQL', () => {
  it('membuka baris menampilkan SQL snapshot yang persis dan menutupnya menyembunyikan lagi', async () => {
    const { user } = openAs('reviewer', history)
    const rows = await bodyRows()
    const button = within(rows[3]).getByRole('button', { name: 'SQL' })
    expect(button).toHaveAttribute('aria-expanded', 'false')
    await user.click(button)
    expect(button).toHaveAttribute('aria-expanded', 'true')
    const region = await screen.findByRole('region', { name: 'Detail eksekusi 2' })
    expect(await within(region).findByRole('textbox', { name: 'Salinan SQL up' })).toHaveAttribute('aria-readonly', 'true')
    expect(await sqlOf('Salinan SQL up')).toBe('ALTER TABLE public.promo ADD COLUMN diskon numeric(5,2) NOT NULL DEFAULT 0;')
    expect(within(region).getByRole('link', { name: 'Lihat permintaan' })).toHaveAttribute('href', `/databases/${ids.inventaris}/review/${workflowIds.changeRequest(2)}`)
    expect(within(region).getByRole('link', { name: 'Lihat job' })).toHaveAttribute('href', `/databases/${ids.inventaris}/jobs/${workflowIds.job(2)}`)
    await user.click(button)
    expect(screen.queryByRole('region', { name: 'Detail eksekusi 2' })).not.toBeInTheDocument()
  })

  it('rollback menampilkan SQL down yang benar-benar dijalankan', async () => {
    const { user } = openAs('reviewer', history)
    const rows = await bodyRows()
    await user.click(within(rows[2]).getByRole('button', { name: 'SQL' }))
    expect(await sqlOf('Salinan SQL down')).toBe('ALTER TABLE public.promo DROP COLUMN diskon;')
  })

  it('beberapa baris bisa terbuka bersamaan', async () => {
    const { user } = openAs('reviewer', history)
    const rows = await bodyRows()
    await user.click(within(rows[0]).getByRole('button', { name: 'SQL' }))
    await user.click(within(rows[4]).getByRole('button', { name: 'SQL' }))
    expect(await screen.findAllByRole('region', { name: /Detail eksekusi/ })).toHaveLength(2)
  })

  it('kegagalan memuat detail ditampilkan di dalam baris dengan tombol coba lagi', async () => {
    const { user } = openAs('reviewer', history)
    const rows = await bodyRows()
    interceptFetch('/executions/', { failures: 1 })
    await user.click(within(rows[0]).getByRole('button', { name: 'SQL' }))
    expect(await screen.findByRole('alert')).toHaveTextContent('Tidak dapat terhubung ke server')
    await user.click(screen.getByRole('button', { name: 'Coba lagi' }))
    expect(await screen.findByRole('textbox', { name: 'Salinan SQL up' })).toBeInTheDocument()
  })
})

// Eksekusi batch terbaru dengan banyak batch ditambahkan ke state mock sehingga halaman statement bisa diuji
function seedBatchExecution(batches: number) {
  const state = getState()
  const latest = state.executions.reduce((a, b) => (a.position_version > b.position_version ? a : b))
  const query = 'UPDATE public.orders SET status = $1 WHERE id IN (SELECT id FROM public.orders WHERE id > $2 AND id <= $3 ORDER BY id LIMIT 500) RETURNING id'
  state.executions.push({
    ...latest,
    id: crypto.randomUUID(),
    position_version: latest.position_version + 1,
    started_at: '2026-11-13T09:59:00Z',
    committed_at: '2026-11-13T10:00:00Z',
    statements: Array.from({ length: batches }, (_, index) => ({ query, params: ['[REDACTED]', '[REDACTED]', '[REDACTED]'], batch: index + 1 })),
  })
}

describe('SQL yang dijalankan', () => {
  const open = async (user: ReturnType<typeof openAs>['user'], row: number) => {
    await user.click(within((await bodyRows())[row]).getByRole('button', { name: 'SQL' }))
    return screen.findByRole('region', { name: /Detail eksekusi/ })
  }

  it('eksekusi normal menampilkan statement tanpa nomor batch dan tanpa halaman lain', async () => {
    const { user } = openAs('reviewer', history)
    const region = await open(user, 3)
    const section = await within(region).findByRole('region', { name: 'SQL yang dijalankan' })
    expect(await within(section).findByText(/ALTER TABLE public\.promo ADD COLUMN diskon/)).toBeInTheDocument()
    expect(within(section).queryByText(/Batch/)).not.toBeInTheDocument()
    expect(within(section).queryByRole('navigation', { name: 'Halaman statement' })).not.toBeInTheDocument()
    expect(within(section).getByText(/disamarkan sebagai \[REDACTED\]/)).toBeInTheDocument()
  })

  it('eksekusi batch menampilkan nomor batch dan membagi statement per halaman', async () => {
    seedBatchExecution(60)
    const { user } = openAs('reviewer', history)
    const section = await within(await open(user, 0)).findByRole('region', { name: 'SQL yang dijalankan' })
    expect(await within(section).findByText('Batch 1')).toBeInTheDocument()
    expect(within(section).getAllByText(/^Batch \d+$/)).toHaveLength(25)
    expect(within(section).getByText('Halaman 1 · statement 1 sampai 25')).toBeInTheDocument()
    expect(within(section).getByRole('button', { name: 'Sebelumnya' })).toBeDisabled()

    await user.click(within(section).getByRole('button', { name: 'Berikutnya' }))
    expect(await within(section).findByText('Batch 26')).toBeInTheDocument()
    expect(within(section).queryByText('Batch 1')).not.toBeInTheDocument()
    expect(within(section).getByText('Halaman 2 · statement 26 sampai 50')).toBeInTheDocument()

    await user.click(within(section).getByRole('button', { name: 'Berikutnya' }))
    expect(await within(section).findByText('Batch 60')).toBeInTheDocument()
    expect(within(section).getAllByText(/^Batch \d+$/)).toHaveLength(10)
    expect(within(section).getByRole('button', { name: 'Berikutnya' })).toBeDisabled()

    await user.click(within(section).getByRole('button', { name: 'Sebelumnya' }))
    expect(await within(section).findByText('Batch 26')).toBeInTheDocument()
  })

  it('hanya meminta satu halaman statement sekali buka, bukan seluruh batch', async () => {
    seedBatchExecution(60)
    const spy = interceptFetch('/statements')
    const { user } = openAs('reviewer', history)
    const section = await within(await open(user, 0)).findByRole('region', { name: 'SQL yang dijalankan' })
    await within(section).findByText('Batch 1')
    const urls = spy.mock.calls.map(([input]) => String(input)).filter((url) => url.includes('/statements'))
    expect(urls).toHaveLength(1)
    expect(urls[0]).toMatch(/limit=25/)
    expect(urls[0]).toMatch(/page=1/)
  })

  it('kegagalan memuat statement tampil di dalam bagiannya dan bisa dicoba lagi', async () => {
    interceptFetch('/statements', { failures: 1 })
    const { user } = openAs('reviewer', history)
    const region = await open(user, 3)
    const section = await within(region).findByRole('region', { name: 'SQL yang dijalankan' })
    expect(await within(section).findByRole('alert')).toHaveTextContent('Tidak dapat terhubung ke server')
    await user.click(within(section).getByRole('button', { name: 'Coba lagi' }))
    expect(await within(section).findByText(/ALTER TABLE public\.promo ADD COLUMN diskon/)).toBeInTheDocument()
  })
})

describe('posisi dan rollback atau forward', () => {
  it('menampilkan migration aktif dan keterangan rollback serta forward', async () => {
    openAs('reviewer', history)
    expect(await screen.findByText('Migration aktif')).toBeInTheDocument()
    expect(screen.getByText('Migration aktif').nextElementSibling).toHaveTextContent('Arsipkan order selesai')
    expect(screen.getByText('Rollback', { selector: 'dt' }).nextElementSibling).toHaveTextContent('Tersedia untuk migration aktif')
    expect(screen.getByText('Forward', { selector: 'dt' }).nextElementSibling).toHaveTextContent('Tidak ada migration')
    expect(screen.getByRole('button', { name: 'Forward' })).toBeDisabled()
    await waitFor(() => expect(screen.getByRole('button', { name: 'Rollback' })).toBeEnabled())
  })

  it('rollback diajukan sebagai permintaan baru untuk eksekusi aktif dan membuka halaman review-nya', async () => {
    const { user } = openAs('reviewer', history)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Rollback' })).toBeEnabled())
    await user.click(screen.getByRole('button', { name: 'Rollback' }))
    const dialog = screen.getByRole('dialog', { name: 'Ajukan rollback' })
    expect(dialog).toHaveTextContent('SQL down dari Arsipkan order selesai')
    await user.click(within(dialog).getByRole('button', { name: 'Ajukan rollback' }))
    expect(await screen.findByText('Menunggu review')).toBeInTheDocument()
    expect(await screen.findByText('Permintaan rollback dibuat dan menunggu approval.')).toBeInTheDocument()
    const created = getState().changeRequests.find((request) => request.kind === 'rollback' && request.status === 'pending_review')
    expect(created).toMatchObject({ migration_id: workflowIds.migrationC, target_execution_id: workflowIds.execution(5) })
  })

  it('setelah rollback dijalankan forward tersedia dan menunjuk migration yang dibatalkan', async () => {
    const state = getState()
    const last = state.executions.at(-1)!
    const rollback = { ...last, id: crypto.randomUUID(), operation: 'rollback' as const, direction: 'down' as const, reverts_execution_id: last.id, forward_of_execution_id: null, position_version: last.position_version + 1 }
    state.executions.push(rollback)
    state.positionVersions[ids.inventaris] = rollback.position_version
    const { user } = openAs('reviewer', history)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Forward' })).toBeEnabled())
    expect(screen.getByText('Forward', { selector: 'dt' }).nextElementSibling).toHaveTextContent('Tersedia: Arsipkan order selesai')
    await user.click(screen.getByRole('button', { name: 'Forward' }))
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Ajukan forward' }))
    await screen.findByText('Menunggu review')
    expect(state.changeRequests.some((request) => request.kind === 'forward' && request.target_execution_id === last.id)).toBe(true)
  })

  it('migration aktif yang irreversible tidak bisa di-rollback dan alasannya dijelaskan', async () => {
    getState().migrations.find((migration) => migration.id === workflowIds.migrationC)!.irreversible = true
    openAs('reviewer', history)
    expect(await screen.findByText(/Tidak tersedia: migration aktif ditandai irreversible/)).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Rollback' })).toBeDisabled()
  })

  it('database yang ditahan job menampilkan peringatan dan mengunci rollback dan forward', async () => {
    const { job } = seedJob()
    job.status = 'partial'
    openAs('reviewer', history)
    const warning = await screen.findByText(/Database sedang ditahan oleh/)
    expect(within(warning.closest('.alert') as HTMLElement).getByRole('link', { name: 'sebuah job' })).toHaveAttribute('href', `/databases/${ids.inventaris}/jobs/${job.id}`)
    expect(screen.getByRole('button', { name: 'Rollback' })).toBeDisabled()
  })

  it('pengguna dengan akses lihat saja melihat posisi tanpa tombol', async () => {
    openAs('observer', history)
    expect(await screen.findByText(/hanya bisa diajukan oleh pengguna dengan akses ubah/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Rollback' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Forward' })).not.toBeInTheDocument()
  })

  it('kegagalan pengajuan tampil di dalam dialog tanpa membuat permintaan', async () => {
    const { user } = openAs('reviewer', history)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Rollback' })).toBeEnabled())
    await user.click(screen.getByRole('button', { name: 'Rollback' }))
    interceptFetch('/change-requests', { failures: 1 })
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Ajukan rollback' }))
    expect(await within(screen.getByRole('dialog')).findByRole('alert')).toHaveTextContent('Tidak dapat terhubung ke server')
    expect(getState().changeRequests.some((request) => request.kind === 'rollback' && request.status === 'pending_review')).toBe(false)
  })

  it('membatalkan dialog tidak mengajukan apa pun', async () => {
    const { user } = openAs('reviewer', history)
    await waitFor(() => expect(screen.getByRole('button', { name: 'Rollback' })).toBeEnabled())
    await user.click(screen.getByRole('button', { name: 'Rollback' }))
    await user.click(screen.getByRole('button', { name: 'Batal' }))
    expect(getState().changeRequests.filter((request) => request.status === 'pending_review')).toHaveLength(0)
  })
})
