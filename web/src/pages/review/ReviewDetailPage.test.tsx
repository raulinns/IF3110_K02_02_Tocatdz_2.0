// @vitest-environment jsdom
import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { handlers } from '../../mocks/handlers/index.ts'
import { ids, workflowIds } from '../../mocks/ids.ts'
import { getState } from '../../mocks/state.ts'
import { setupMockServer } from '../../mocks/testing.ts'
import { openAs } from '../../test/app.tsx'
import { sqlOf } from '../../test/editor.ts'
import { interceptFetch } from '../../test/network.ts'
import { approveAll, seedRequest } from '../../test/workflow.ts'

setupMockServer(...handlers)

const detail = (id: string) => `/databases/${ids.inventaris}/review/${id}`
const approvals = async () => within(await screen.findByRole('table', { name: 'Approver per group' }))
const rowOf = async (group: string) => (await approvals()).getByText(group).closest('tr') as HTMLElement
const requestOf = (id: string) => getState().changeRequests.find((request) => request.id === id)!

describe('detail permintaan', () => {
  it('menampilkan jenis, status, SQL yang akan dijalankan, dan tabel approver per group', async () => {
    const request = seedRequest()
    openAs('observer', detail(request.id))
    expect(await screen.findByRole('heading', { name: 'Tambah indeks status order' })).toBeInTheDocument()
    expect(screen.getByText('Terapkan')).toBeInTheDocument()
    expect(screen.getByText('Menunggu review')).toBeInTheDocument()
    expect(await sqlOf('SQL up')).toBe('CREATE INDEX idx_orders_status ON public.orders (status);')
    expect(screen.getByRole('textbox', { name: 'SQL up' })).toHaveAttribute('aria-readonly', 'true')
    const writers = await rowOf('writers')
    expect(within(writers).getByText('Lihat + ubah')).toBeInTheDocument()
    expect(within(writers).getByText('Menunggu')).toBeInTheDocument()
    expect(within(await rowOf('readers')).getByText('Lihat saja')).toBeInTheDocument()
    expect(screen.getByText(/Menunggu keputusan dari 2 group lagi/)).toBeInTheDocument()
    expect(screen.getByText('0/2 group menyetujui')).toBeInTheDocument()
  })

  it('rollback menampilkan SQL down dan forward menampilkan SQL up', async () => {
    const rollback = seedRequest({ kind: 'rollback', migration_id: workflowIds.migrationA })
    const view = openAs('reviewer', detail(rollback.id))
    expect(await screen.findByText('SQL down', { selector: '.card-header-meta' })).toBeInTheDocument()
    expect(await sqlOf('SQL down')).toBe('DROP TABLE public.promo;')
    view.unmount()
    const forward = seedRequest({ kind: 'forward', migration_id: workflowIds.migrationB })
    openAs('reviewer', detail(forward.id))
    expect(await screen.findByText('SQL up', { selector: '.card-header-meta' })).toBeInTheDocument()
  })

  it('migration batch menampilkan konfigurasi batch dan migration irreversible menampilkan alasannya', async () => {
    const batch = seedRequest({ migration_id: workflowIds.migrationC })
    const view = openAs('reviewer', detail(batch.id))
    expect(await screen.findByText('public.orders')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Bagian SET' })).toBeInTheDocument()
    view.unmount()
    const irreversible = seedRequest({ migration_id: workflowIds.migrationE })
    openAs('reviewer', detail(irreversible.id))
    expect(await screen.findByText('Data kolom yang dihapus tidak dapat dipulihkan.')).toBeInTheDocument()
  })

  it('checksum yang berbeda dari definisi saat ini diperingatkan', async () => {
    const request = seedRequest({ checksum: 'a'.repeat(64) })
    openAs('reviewer', detail(request.id))
    expect(await screen.findByText(/Checksum migration saat ini berbeda dari yang direview/)).toBeInTheDocument()
  })

  it('leader yang bukan pengaju melihat tombol keputusan hanya pada groupnya sendiri', async () => {
    const request = seedRequest()
    openAs('reviewer', detail(request.id))
    expect(within(await rowOf('writers')).getByRole('button', { name: 'Putuskan' })).toBeEnabled()
    expect(within(await rowOf('readers')).queryByRole('button', { name: 'Putuskan' })).not.toBeInTheDocument()
    expect(within(await rowOf('writers')).getByText('reviewer (Anda)')).toBeInTheDocument()
  })

  it('pengguna yang bukan leader tidak melihat tombol keputusan', async () => {
    const request = seedRequest()
    openAs('editor', detail(request.id))
    await approvals()
    expect(screen.queryByRole('button', { name: 'Putuskan' })).not.toBeInTheDocument()
  })

  it('pengaju yang juga leader tidak bisa memutuskan permintaannya sendiri', async () => {
    const request = seedRequest({ requested_by: ids.reviewer })
    openAs('reviewer', detail(request.id))
    const writers = await rowOf('writers')
    expect(within(writers).getByRole('button', { name: 'Putuskan' })).toBeDisabled()
    expect(within(writers).getByText('Anda pengaju')).toBeInTheDocument()
  })

  it('migration yang tidak ada atau id yang salah menampilkan halaman yang sesuai', async () => {
    const missing = openAs('reviewer', detail('50000000-0000-4000-8000-000000009999'))
    expect(await screen.findByText('Tidak ditemukan')).toBeInTheDocument()
    missing.unmount()
    openAs('reviewer', detail('bukan-uuid'))
    expect(await screen.findByRole('alert')).toHaveTextContent('Permintaan tidak valid')
  })
})

describe('memutuskan', () => {
  it('menyetujui mencatat keputusan dan catatan tanpa mengubah status sebelum semua group setuju', async () => {
    const request = seedRequest()
    const { user } = openAs('reviewer', detail(request.id))
    await user.click(within(await rowOf('writers')).getByRole('button', { name: 'Putuskan' }))
    const dialog = screen.getByRole('dialog', { name: /Keputusan untuk group writers/ })
    expect(within(dialog).getByLabelText('Catatan (opsional)')).toHaveFocus()
    await user.type(within(dialog).getByLabelText('Catatan (opsional)'), '  Sudah dicek  ')
    await user.click(within(dialog).getByRole('button', { name: 'Setujui' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Anda menyetujui atas nama group writers.')
    const writers = await rowOf('writers')
    expect(within(writers).getByText('Disetujui')).toBeInTheDocument()
    expect(within(writers).getByText('Sudah dicek')).toBeInTheDocument()
    expect(within(writers).queryByRole('button', { name: 'Putuskan' })).not.toBeInTheDocument()
    expect(screen.getByText('1/2 group menyetujui')).toBeInTheDocument()
    expect(requestOf(request.id).status).toBe('pending_review')
  })

  it('persetujuan group terakhir membuat permintaan disetujui dan menawarkan tombol jalankan', async () => {
    const request = seedRequest()
    Object.assign(request.approvals[0], { decision: 'approve', actor_id: ids.reviewer })
    const { user } = openAs('observer', detail(request.id))
    await user.click(within(await rowOf('readers')).getByRole('button', { name: 'Putuskan' }))
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Setujui' }))
    expect(within(await rowOf('readers')).getByText('Disetujui')).toBeInTheDocument()
    // Leader readers hanya punya akses lihat sehingga tidak boleh menjalankan
    expect(await screen.findByText(/Hanya pengguna dengan akses ubah/)).toBeInTheDocument()
    expect(requestOf(request.id).status).toBe('approved')
    expect(screen.queryByRole('button', { name: 'Jalankan' })).not.toBeInTheDocument()
  })

  it('menolak membuat seluruh permintaan ditolak dan alasannya terlihat', async () => {
    const request = seedRequest()
    const { user } = openAs('reviewer', detail(request.id))
    await user.click(within(await rowOf('writers')).getByRole('button', { name: 'Putuskan' }))
    await user.type(screen.getByLabelText('Catatan (opsional)'), 'Terlalu berisiko')
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Tolak' }))
    expect(await screen.findByText(/Permintaan ditolak oleh group writers/)).toHaveTextContent('Catatan: Terlalu berisiko')
    expect(requestOf(request.id).status).toBe('rejected')
    expect(within(await rowOf('writers')).getByText('Ditolak')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Jalankan' })).not.toBeInTheDocument()
  })

  it('membatalkan dialog tidak mencatat apa pun', async () => {
    const request = seedRequest()
    const { user } = openAs('reviewer', detail(request.id))
    await user.click(within(await rowOf('writers')).getByRole('button', { name: 'Putuskan' }))
    await user.click(screen.getByRole('button', { name: 'Batal' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(requestOf(request.id).approvals[0].decision).toBeNull()
  })

  it('catatan yang terlalu panjang ditolak di browser tanpa request', async () => {
    const request = seedRequest()
    const spy = interceptFetch('/approvals')
    const { user } = openAs('reviewer', detail(request.id))
    await user.click(within(await rowOf('writers')).getByRole('button', { name: 'Putuskan' }))
    fireEvent.change(screen.getByLabelText('Catatan (opsional)'), { target: { value: 'x'.repeat(1001) } })
    await user.click(screen.getByRole('button', { name: 'Setujui' }))
    expect(screen.getByText('Catatan maksimal 1000 karakter')).toBeInTheDocument()
    expect(spy.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(0)
  })

  it('permintaan yang sudah berubah di server ditolak dan bisa dimuat ulang dari dialog', async () => {
    const request = seedRequest()
    const { user } = openAs('reviewer', detail(request.id))
    await user.click(within(await rowOf('writers')).getByRole('button', { name: 'Putuskan' }))
    requestOf(request.id).revision = 4
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Setujui' }))
    expect(await within(screen.getByRole('dialog')).findByRole('alert')).toHaveTextContent('sudah diubah')
    await user.click(screen.getByRole('button', { name: 'Muat ulang permintaan' }))
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
    expect(await screen.findByText('4', { selector: '.mono' })).toBeInTheDocument()
  })

  it('leader yang sudah diganti sejak pengajuan tidak bisa memutuskan dengan penjelasan', async () => {
    const request = seedRequest()
    getState().groups.find((group) => group.id === ids.writers)!.leader_user_id = ids.editor
    const { user } = openAs('reviewer', detail(request.id))
    await user.click(within(await rowOf('writers')).getByRole('button', { name: 'Putuskan' }))
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Setujui' }))
    expect(await within(screen.getByRole('dialog')).findByRole('alert')).toHaveTextContent('Daftar approver berubah sejak permintaan diajukan')
    expect(requestOf(request.id).approvals[0].decision).toBeNull()
  })

  it('menekan tombol dua kali dengan cepat hanya mengirim satu keputusan', async () => {
    const request = seedRequest()
    const spy = interceptFetch('/approvals', { delay: 100 })
    const { user } = openAs('reviewer', detail(request.id))
    await user.click(within(await rowOf('writers')).getByRole('button', { name: 'Putuskan' }))
    const button = within(screen.getByRole('dialog')).getByRole('button', { name: 'Setujui' })
    fireEvent.click(button)
    fireEvent.click(button)
    await screen.findByRole('status')
    expect(spy.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(1)
  })

  it('kegagalan jaringan tampil di dalam dialog yang tetap terbuka', async () => {
    const request = seedRequest()
    const { user } = openAs('reviewer', detail(request.id))
    await user.click(within(await rowOf('writers')).getByRole('button', { name: 'Putuskan' }))
    interceptFetch('/approvals', { failures: 1 })
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Setujui' }))
    expect(await within(screen.getByRole('dialog')).findByRole('alert')).toHaveTextContent('Tidak dapat terhubung ke server')
    expect(screen.getByRole('button', { name: 'Setujui' })).toBeEnabled()
  })
})

describe('menjalankan', () => {
  it('permintaan yang disetujui diantrekan setelah konfirmasi dan menghasilkan job', async () => {
    const request = seedRequest()
    approveAll(request)
    const { user } = openAs('reviewer', detail(request.id))
    await user.click(await screen.findByRole('button', { name: 'Jalankan' }))
    const dialog = screen.getByRole('dialog', { name: 'Jalankan permintaan' })
    expect(dialog).toHaveTextContent('satu transaksi')
    await user.click(within(dialog).getByRole('button', { name: 'Jalankan' }))
    await waitFor(() => expect(getState().jobs.some((candidate) => candidate.change_request_id === request.id)).toBe(true))
    expect(requestOf(request.id).status).toBe('queued')
  })

  it('membatalkan konfirmasi tidak membuat job', async () => {
    const request = seedRequest()
    approveAll(request)
    const { user } = openAs('reviewer', detail(request.id))
    await user.click(await screen.findByRole('button', { name: 'Jalankan' }))
    await user.click(screen.getByRole('button', { name: 'Batal' }))
    expect(getState().jobs.some((candidate) => candidate.change_request_id === request.id)).toBe(false)
  })

  it('posisi database yang sudah berubah menggagalkan jalankan dengan penjelasan di dialog', async () => {
    const request = seedRequest()
    approveAll(request)
    request.expected_position_version = 99
    const { user } = openAs('reviewer', detail(request.id))
    await user.click(await screen.findByRole('button', { name: 'Jalankan' }))
    await user.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Jalankan' }))
    expect(await within(screen.getByRole('dialog')).findByRole('alert')).toHaveTextContent('Posisi migration sudah berubah')
    expect(requestOf(request.id).status).toBe('approved')
  })

  it('permintaan batch menjelaskan eksekusi bertahap di dialog', async () => {
    const request = seedRequest({ migration_id: workflowIds.migrationC })
    approveAll(request)
    const { user } = openAs('reviewer', detail(request.id))
    await user.click(await screen.findByRole('button', { name: 'Jalankan' }))
    expect(screen.getByRole('dialog')).toHaveTextContent('dijalankan bertahap')
  })

  it('permintaan yang sudah berjalan menautkan ke monitor job', async () => {
    const request = seedRequest({ status: 'succeeded' })
    const job = { ...getState().jobs[0], id: crypto.randomUUID(), change_request_id: request.id, migration_id: request.migration_id }
    getState().jobs.push(job)
    openAs('reviewer', detail(request.id))
    expect(await screen.findByRole('link', { name: 'Buka monitor job' })).toHaveAttribute('href', `/databases/${ids.inventaris}/jobs/${job.id}`)
  })
})
