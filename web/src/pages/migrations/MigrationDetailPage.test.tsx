// @vitest-environment jsdom
import { fireEvent, screen, waitFor, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { handlers } from '../../mocks/handlers/index.ts'
import { ids, workflowIds } from '../../mocks/ids.ts'
import { getState } from '../../mocks/state.ts'
import { setupMockServer } from '../../mocks/testing.ts'
import { openAs } from '../../test/app.tsx'
import { setSql, sqlOf } from '../../test/editor.ts'
import { interceptFetch } from '../../test/network.ts'

setupMockServer(...handlers)

const detail = (id: string) => `/databases/${ids.inventaris}/migrations/${id}`
const draft = workflowIds.migrationD
const executed = workflowIds.migrationA
const batchExecuted = workflowIds.migrationC
const irreversible = workflowIds.migrationE
const migrationOf = (id: string) => getState().migrations.find((candidate) => candidate.id === id)!

type User = ReturnType<typeof openAs>['user']
const ready = () => screen.findByRole('textbox', { name: 'SQL up' })
const save = (user: User) => user.click(screen.getByRole('button', { name: 'Simpan draft' }))
const retitle = async (user: User, title: string) => {
  await user.clear(screen.getByLabelText('Judul'))
  if (title) await user.type(screen.getByLabelText('Judul'), title)
}

describe('detail draft', () => {
  it('menampilkan definisi draft yang bisa diubah dengan status, mode, revision, dan checksum', async () => {
    openAs('reviewer', detail(draft))
    expect(await screen.findByRole('heading', { name: 'Tambah indeks status order' })).toBeInTheDocument()
    await ready()
    expect(await sqlOf('SQL up')).toBe('CREATE INDEX idx_orders_status ON public.orders (status);')
    expect(await sqlOf('SQL down')).toBe('DROP INDEX public.idx_orders_status;')
    expect(screen.getByLabelText('Judul')).toHaveValue('Tambah indeks status order')
    expect(screen.getByLabelText('Normal')).toBeChecked()
    expect(screen.getByText('Draft')).toBeInTheDocument()
    expect(screen.getByTitle(migrationOf(draft).checksum)).toHaveTextContent(migrationOf(draft).checksum.slice(0, 12))
    expect(screen.getByRole('button', { name: 'Simpan draft' })).toBeEnabled()
    expect(screen.getByRole('heading', { name: 'EXPLAIN' })).toBeInTheDocument()
  })

  it('migration yang tidak ada menampilkan halaman tidak ditemukan', async () => {
    openAs('reviewer', detail('20000000-0000-4000-8000-000000009999'))
    expect(await screen.findByText('Tidak ditemukan')).toBeInTheDocument()
  })

  it('id yang bukan UUID menampilkan pesan kesalahan dengan tombol coba lagi', async () => {
    openAs('reviewer', detail('bukan-migration'))
    expect(await screen.findByRole('alert')).toHaveTextContent('Permintaan tidak valid')
    expect(screen.getByRole('button', { name: 'Coba lagi' })).toBeInTheDocument()
  })

  it('menyimpan perubahan menaikkan revision dan menampilkan pesan', async () => {
    const { user } = openAs('reviewer', detail(draft))
    await ready()
    await retitle(user, 'Judul baru')
    await setSql('SQL up', 'CREATE INDEX idx_baru ON public.orders (status);')
    expect(screen.getByText('Ada perubahan yang belum disimpan')).toBeInTheDocument()
    await save(user)
    expect(await screen.findByRole('status')).toHaveTextContent('Draft disimpan sebagai revision 2.')
    expect(migrationOf(draft)).toMatchObject({ title: 'Judul baru', up_sql: 'CREATE INDEX idx_baru ON public.orders (status);', revision: 2 })
    expect(screen.getByRole('heading', { name: 'Judul baru' })).toBeInTheDocument()
    await ready()
    expect(screen.queryByText('Ada perubahan yang belum disimpan')).not.toBeInTheDocument()
  })

  it('judul yang kosong ditolak di browser tanpa mengirim request dan fokus ke isian itu', async () => {
    const spy = interceptFetch('/migrations/')
    const { user } = openAs('reviewer', detail(draft))
    await ready()
    await retitle(user, '')
    await save(user)
    expect(screen.getByText('Judul wajib diisi')).toBeInTheDocument()
    expect(screen.getByLabelText('Judul')).toHaveFocus()
    expect(spy.mock.calls.filter(([, init]) => init?.method === 'PATCH')).toHaveLength(0)
  })

  it('SQL down yang dikosongkan diminta diisi atau ditandai irreversible', async () => {
    const { user } = openAs('reviewer', detail(draft))
    await ready()
    await setSql('SQL down', '')
    await save(user)
    expect(screen.getByText('SQL down wajib diisi atau tandai migration irreversible')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'SQL down' })).toHaveAttribute('aria-invalid', 'true')
  })

  it('error validasi server ditampilkan tepat di editor yang bermasalah dan editor itu mendapat fokus', async () => {
    const { user } = openAs('reviewer', detail(draft))
    await ready()
    await setSql('SQL up', 'BEGIN; SELECT 1;')
    await save(user)
    expect(await screen.findByText('Perintah transaksi diatur oleh aplikasi.')).toBeInTheDocument()
    const up = screen.getByRole('textbox', { name: 'SQL up' })
    expect(up).toHaveAttribute('aria-invalid', 'true')
    expect(up).toHaveFocus()
    expect(screen.getByRole('textbox', { name: 'SQL down' })).not.toHaveAttribute('aria-invalid')
    expect(migrationOf(draft).revision).toBe(1)
  })

  it('error pada kedua editor tampil bersamaan dan hilang saat editornya diperbaiki', async () => {
    const { user } = openAs('reviewer', detail(draft))
    await ready()
    await setSql('SQL up', 'COMMIT;')
    await setSql('SQL down', 'ROLLBACK;')
    await save(user)
    expect(await screen.findAllByText('Perintah transaksi diatur oleh aplikasi.')).toHaveLength(2)
    await setSql('SQL up', 'SELECT 1;')
    await waitFor(() => expect(screen.getAllByText('Perintah transaksi diatur oleh aplikasi.')).toHaveLength(1))
  })

  it('menyimpan dua kali dengan cepat hanya mengirim satu request', async () => {
    const spy = interceptFetch('/migrations/', { delay: 100 })
    const { user } = openAs('reviewer', detail(draft))
    await ready()
    await retitle(user, 'Sekali saja')
    const button = screen.getByRole('button', { name: 'Simpan draft' })
    // Submit form dikirim dua kali seperti menekan Enter berturut-turut karena tombol yang terkunci tidak menerima klik kedua
    const form = button.closest('form') as HTMLFormElement
    fireEvent.submit(form)
    fireEvent.submit(form)
    await screen.findByRole('status')
    expect(spy.mock.calls.filter(([, init]) => init?.method === 'PATCH')).toHaveLength(1)
  })

  it('meminta konfirmasi peramban saat tab ditutup dengan perubahan yang belum disimpan', async () => {
    const { user } = openAs('reviewer', detail(draft))
    await ready()
    const clean = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(clean)
    expect(clean.defaultPrevented).toBe(false)
    await retitle(user, 'Berubah')
    const dirty = new Event('beforeunload', { cancelable: true })
    window.dispatchEvent(dirty)
    expect(dirty.defaultPrevented).toBe(true)
  })

  it('tombol ajukan terkunci selama ada perubahan yang belum disimpan', async () => {
    const { user } = openAs('reviewer', detail(draft))
    await ready()
    expect(screen.getByRole('button', { name: 'Ajukan untuk review' })).toBeEnabled()
    await retitle(user, 'Berubah')
    expect(screen.getByRole('button', { name: 'Ajukan untuk review' })).toBeDisabled()
  })
})

describe('konflik dengan server', () => {
  it('revision yang sudah berubah ditolak dan editor bisa dimuat ulang dari server', async () => {
    const { user } = openAs('reviewer', detail(draft))
    await ready()
    await retitle(user, 'Perubahan saya')
    migrationOf(draft).revision = 5
    migrationOf(draft).title = 'Perubahan orang lain'
    await save(user)
    expect(await screen.findByRole('alert')).toHaveTextContent('Migration sudah diubah')
    await user.click(screen.getByRole('button', { name: 'Muat ulang dari server' }))
    expect(await screen.findByRole('heading', { name: 'Perubahan orang lain' })).toBeInTheDocument()
    expect(screen.getByLabelText('Judul')).toHaveValue('Perubahan orang lain')
    expect(screen.queryByRole('alert')).not.toBeInTheDocument()
  })

  it('draft yang sudah dibekukan server ditolak dan setelah dimuat ulang halaman menjadi baca saja', async () => {
    const { user } = openAs('reviewer', detail(draft))
    await ready()
    await retitle(user, 'Terlambat')
    migrationOf(draft).state = 'frozen'
    await save(user)
    expect(await screen.findByRole('alert')).toHaveTextContent('dibekukan')
    await user.click(screen.getByRole('button', { name: 'Muat ulang dari server' }))
    expect(await screen.findByText(/Definisi dibekukan karena sudah diajukan/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Simpan draft' })).not.toBeInTheDocument()
  })

  it('kegagalan jaringan ditampilkan dan isian pengguna tidak hilang', async () => {
    const { user } = openAs('reviewer', detail(draft))
    await ready()
    await retitle(user, 'Tetap ada')
    interceptFetch('/migrations/', { failures: 1 })
    await save(user)
    expect(await screen.findByRole('alert')).toHaveTextContent('Tidak dapat terhubung ke server')
    expect(screen.getByLabelText('Judul')).toHaveValue('Tetap ada')
    expect(screen.queryByRole('button', { name: 'Muat ulang dari server' })).not.toBeInTheDocument()
  })
})

describe('irreversible dan mode batch', () => {
  it('menandai irreversible menyembunyikan SQL down dan meminta alasan', async () => {
    const { user } = openAs('reviewer', detail(draft))
    await ready()
    await user.click(screen.getByLabelText('Irreversible (tidak dapat dibatalkan)'))
    expect(screen.queryByRole('textbox', { name: 'SQL down' })).not.toBeInTheDocument()
    await save(user)
    expect(screen.getByText('Alasan irreversible wajib diisi')).toBeInTheDocument()
    expect(screen.getByLabelText('Alasan irreversible')).toHaveFocus()
    await user.type(screen.getByLabelText('Alasan irreversible'), 'Data tidak bisa dipulihkan')
    await save(user)
    expect(await screen.findByRole('status')).toHaveTextContent('revision 2')
    expect(migrationOf(draft)).toMatchObject({ irreversible: true, down_sql: null, irreversible_reason: 'Data tidak bisa dipulihkan' })
  })

  it('migration irreversible yang tersimpan menampilkan alasan dan tanpa editor down', async () => {
    openAs('reviewer', detail(irreversible))
    await ready()
    expect(screen.getByLabelText('Irreversible (tidak dapat dibatalkan)')).toBeChecked()
    expect(screen.getByLabelText('Alasan irreversible')).toHaveValue('Data kolom yang dihapus tidak dapat dipulihkan.')
    expect(screen.queryByRole('textbox', { name: 'SQL down' })).not.toBeInTheDocument()
    expect(screen.getAllByText('Irreversible').length).toBeGreaterThan(0)
  })

  it('membatalkan irreversible mengembalikan SQL down yang sempat diketik', async () => {
    const { user } = openAs('reviewer', detail(draft))
    await ready()
    await setSql('SQL down', 'DROP INDEX public.x;')
    await user.click(screen.getByLabelText('Irreversible (tidak dapat dibatalkan)'))
    await user.click(screen.getByLabelText('Irreversible (tidak dapat dibatalkan)'))
    expect(await sqlOf('SQL down')).toBe('DROP INDEX public.x;')
  })

  it('mode batch menampilkan form konfigurasi dan mengganti label editor menjadi bagian SET', async () => {
    const { user } = openAs('reviewer', detail(draft))
    await ready()
    expect(screen.queryByLabelText('Tabel')).not.toBeInTheDocument()
    await user.click(screen.getByLabelText('Batch'))
    expect(screen.getByLabelText('Schema')).toHaveValue('public')
    expect(screen.getByLabelText('Kolom kunci')).toHaveValue('id')
    expect(screen.getByLabelText('Ukuran batch')).toHaveValue(1000)
    expect(screen.getByRole('textbox', { name: 'Bagian SET (up)' })).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'Bagian SET (down)' })).toBeInTheDocument()
  })

  it('konfigurasi batch yang salah ditandai per field di browser', async () => {
    const { user } = openAs('reviewer', detail(draft))
    await ready()
    await user.click(screen.getByLabelText('Batch'))
    await setSql('Bagian SET (up)', "status = 'arsip'")
    await setSql('Bagian SET (down)', "status = 'aktif'")
    await user.clear(screen.getByLabelText('Schema'))
    await user.type(screen.getByLabelText('Tabel'), 'bad name')
    await user.clear(screen.getByLabelText('Ukuran batch'))
    await user.type(screen.getByLabelText('Ukuran batch'), '5')
    await save(user)
    expect(screen.getByText('Schema wajib diisi')).toBeInTheDocument()
    expect(screen.getByText('Tabel harus satu identifier valid, maksimal 63 byte')).toBeInTheDocument()
    expect(screen.getByText('Ukuran batch harus bilangan bulat 100 sampai 100000')).toBeInTheDocument()
    expect(screen.getByLabelText('Schema')).toHaveFocus()
  })

  it('menyimpan migration batch yang valid mengirim konfigurasi batch', async () => {
    const { user } = openAs('reviewer', detail(draft))
    await ready()
    await user.click(screen.getByLabelText('Batch'))
    await setSql('Bagian SET (up)', "status = 'arsip'")
    await setSql('Bagian SET (down)', "status = 'aktif'")
    await user.type(screen.getByLabelText('Tabel'), 'orders')
    await save(user)
    expect(await screen.findByRole('status')).toHaveTextContent('revision 2')
    expect(migrationOf(draft)).toMatchObject({ mode: 'batch', batch: { schema: 'public', table: 'orders', key_column: 'id', batch_size: 1000, scope_where: null } })
  })

  it('error batch dari server ditampilkan pada field yang sesuai', async () => {
    const { user } = openAs('reviewer', detail(draft))
    await ready()
    await user.click(screen.getByLabelText('Batch'))
    await setSql('Bagian SET (up)', "UPDATE orders SET status = 'x'")
    await setSql('Bagian SET (down)', "status = 'aktif'")
    await user.type(screen.getByLabelText('Tabel'), 'orders')
    await user.type(screen.getByLabelText('Predikat scope (opsional)'), 'a = 1; DROP TABLE x')
    await save(user)
    expect(await screen.findByText('Isi hanya bagian SET tanpa kata UPDATE, SET, INSERT, atau DELETE.')).toBeInTheDocument()
    expect(screen.getByText('Predikat harus berupa satu ekspresi tanpa subquery atau perintah lain')).toBeInTheDocument()
    expect(screen.getByLabelText('Predikat scope (opsional)')).toHaveAttribute('aria-invalid', 'true')
  })
})

describe('baca saja', () => {
  it('migration yang sudah dijalankan tidak bisa diubah dan menawarkan salinan', async () => {
    openAs('reviewer', detail(executed))
    expect(await screen.findByText(/Migration ini pernah dijalankan sehingga definisinya tidak bisa diubah/)).toBeInTheDocument()
    expect(await ready()).toHaveAttribute('aria-readonly', 'true')
    expect(screen.getByLabelText('Judul')).toHaveAttribute('readonly')
    expect(screen.queryByRole('button', { name: 'Simpan draft' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Ajukan untuk review' })).not.toBeInTheDocument()
    expect(screen.getByText('Dibekukan')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Salin ke draft baru' })).toBeInTheDocument()
  })

  it('migration batch yang dibekukan menampilkan konfigurasinya tanpa bisa diubah', async () => {
    openAs('reviewer', detail(batchExecuted))
    await screen.findByRole('textbox', { name: 'Bagian SET (up)' })
    expect(screen.getByLabelText('Batch')).toBeChecked()
    expect(screen.getByLabelText('Ukuran batch')).toHaveValue(100)
    expect(screen.getByLabelText('Predikat scope (opsional)')).toHaveValue("status = 'selesai'")
    expect(screen.getByLabelText('Tabel')).toHaveAttribute('readonly')
    expect(screen.getByLabelText('Predikat scope (opsional)')).toHaveAttribute('readonly')
    expect(screen.getByLabelText('Ukuran batch')).toHaveAttribute('readonly')
    expect(screen.getByLabelText('Batch')).toBeDisabled()
  })

  it('pengguna dengan akses lihat saja tidak melihat tombol ubah maupun panel EXPLAIN', async () => {
    openAs('observer', detail(draft))
    expect(await screen.findByText(/hanya memiliki akses lihat/)).toBeInTheDocument()
    expect((await ready()).getAttribute('aria-readonly')).toBe('true')
    expect(screen.queryByRole('button', { name: 'Simpan draft' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Salin ke draft baru' })).not.toBeInTheDocument()
    expect(screen.queryByRole('heading', { name: 'EXPLAIN' })).not.toBeInTheDocument()
  })

  it('Salin ke draft baru membuka editor baru berisi salinan dengan judul bertanda salinan', async () => {
    const { user } = openAs('reviewer', detail(executed))
    await user.click(await screen.findByRole('button', { name: 'Salin ke draft baru' }))
    expect(await screen.findByRole('heading', { name: 'Draft baru' })).toBeInTheDocument()
    expect(screen.getByText(/Salinan dari migration/)).toHaveTextContent('Buat tabel promo')
    expect(screen.getByLabelText('Judul')).toHaveValue('Buat tabel promo (salinan)')
    await ready()
    expect(await sqlOf('SQL up')).toBe(migrationOf(executed).up_sql)
    expect(await sqlOf('SQL down')).toBe(migrationOf(executed).down_sql)
  })
})

describe('mengajukan untuk review', () => {
  it('membekukan definisi dan menautkan ke tab Review', async () => {
    const { user } = openAs('reviewer', detail(draft))
    await ready()
    await user.click(screen.getByRole('button', { name: 'Ajukan untuk review' }))
    const dialog = screen.getByRole('dialog', { name: 'Ajukan untuk review' })
    expect(dialog).toHaveTextContent('dibekukan')
    await user.click(within(dialog).getByRole('button', { name: 'Ajukan' }))
    expect(await screen.findByText(/Permintaan review dibuat/)).toBeInTheDocument()
    // Tautan Review juga ada di tab database sehingga dicari di dalam pesan sukses
    const message = screen.getByText(/Permintaan review dibuat/).closest('.alert') as HTMLElement
    expect(within(message).getByRole('link', { name: 'Review' })).toHaveAttribute('href', `/databases/${ids.inventaris}/review`)
    expect(await screen.findByText(/Definisi dibekukan karena sudah diajukan/)).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Simpan draft' })).not.toBeInTheDocument()
    expect(migrationOf(draft).state).toBe('frozen')
    expect(getState().changeRequests.filter((request) => request.migration_id === draft)).toHaveLength(1)
  })

  it('membatalkan dialog tidak mengajukan apa pun', async () => {
    const { user } = openAs('reviewer', detail(draft))
    await ready()
    await user.click(screen.getByRole('button', { name: 'Ajukan untuk review' }))
    await user.click(screen.getByRole('button', { name: 'Batal' }))
    expect(migrationOf(draft).state).toBe('draft')
    expect(getState().changeRequests.some((request) => request.migration_id === draft)).toBe(false)
  })

  it('group tanpa leader menggagalkan pengajuan dengan penjelasan di dalam dialog', async () => {
    getState().groups.find((group) => group.id === ids.writers)!.leader_user_id = null
    const { user } = openAs('reviewer', detail(draft))
    await ready()
    await user.click(screen.getByRole('button', { name: 'Ajukan untuk review' }))
    const dialog = screen.getByRole('dialog')
    await user.click(within(dialog).getByRole('button', { name: 'Ajukan' }))
    expect(await within(dialog).findByRole('alert')).toHaveTextContent('belum memiliki leader')
    expect(migrationOf(draft).state).toBe('draft')
  })
})
