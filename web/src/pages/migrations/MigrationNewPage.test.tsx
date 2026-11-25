// @vitest-environment jsdom
import { fireEvent, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { emptyForm } from '../../lib/definition.ts'
import { handlers } from '../../mocks/handlers/index.ts'
import { ids } from '../../mocks/ids.ts'
import { getState } from '../../mocks/state.ts'
import { setupMockServer } from '../../mocks/testing.ts'
import { openAs } from '../../test/app.tsx'
import { setSql, sqlOf } from '../../test/editor.ts'
import { interceptFetch } from '../../test/network.ts'

setupMockServer(...handlers)

const page = `/databases/${ids.inventaris}/migrations/new`
type User = ReturnType<typeof openAs>['user']

const ready = () => screen.findByRole('textbox', { name: 'SQL up' })
const save = (user: User) => user.click(screen.getByRole('button', { name: 'Simpan draft' }))
const created = () => getState().migrations.filter((migration) => migration.title === 'Tambah kolom telepon')

async function fillNormal(user: User) {
  await ready()
  await user.type(screen.getByLabelText('Judul'), 'Tambah kolom telepon')
  await setSql('SQL up', 'ALTER TABLE public.customers ADD COLUMN telepon text;')
  await setSql('SQL down', 'ALTER TABLE public.customers DROP COLUMN telepon;')
}

describe('buat migration', () => {
  it('menampilkan form kosong dengan mode normal sebagai bawaan', async () => {
    openAs('reviewer', page)
    expect(await screen.findByRole('heading', { name: 'Draft baru' })).toBeInTheDocument()
    await ready()
    expect(screen.getByLabelText('Judul')).toHaveValue('')
    expect(screen.getByLabelText('Normal')).toBeChecked()
    expect(await sqlOf('SQL up')).toBe('')
    expect(screen.getByLabelText('Irreversible (tidak dapat dibatalkan)')).not.toBeChecked()
    expect(screen.queryByLabelText('Tabel')).not.toBeInTheDocument()
  })

  it('pengguna dengan akses lihat saja mendapat halaman akses ditolak', async () => {
    openAs('observer', page)
    expect(await screen.findByText(/akses ditolak/i)).toBeInTheDocument()
    expect(screen.queryByRole('textbox', { name: 'SQL up' })).not.toBeInTheDocument()
  })

  it('menyimpan draft baru lalu membuka halaman detailnya dengan pesan berhasil', async () => {
    const { user } = openAs('reviewer', page)
    await fillNormal(user)
    await save(user)
    expect(await screen.findByRole('status')).toHaveTextContent('Draft berhasil dibuat.')
    expect(await screen.findByRole('heading', { name: 'Tambah kolom telepon' })).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Ajukan untuk review' })).toBeInTheDocument()
    const [migration] = created()
    expect(migration).toMatchObject({ database_id: ids.inventaris, state: 'draft', revision: 1, mode: 'normal', irreversible: false, batch: null })
    expect(migration.up_sql).toBe('ALTER TABLE public.customers ADD COLUMN telepon text;')
    expect(migration.created_by).toBe(ids.reviewer)
  })

  it('draft baru muncul di daftar migration', async () => {
    const { user } = openAs('reviewer', page)
    await fillNormal(user)
    await save(user)
    await screen.findByRole('heading', { name: 'Tambah kolom telepon' })
    await user.click(screen.getAllByRole('link', { name: 'Migration' })[0])
    expect(await screen.findByRole('link', { name: 'Tambah kolom telepon' })).toBeInTheDocument()
  })

  it('SQL dan judul disimpan apa adanya tanpa dipangkas', async () => {
    const { user } = openAs('reviewer', page)
    await ready()
    await user.type(screen.getByLabelText('Judul'), 'Tambah kolom telepon')
    await setSql('SQL up', '\n  ALTER TABLE public.customers ADD COLUMN telepon text;  \n')
    await setSql('SQL down', 'ALTER TABLE public.customers DROP COLUMN telepon;')
    await save(user)
    await screen.findByRole('status')
    expect(created()[0].up_sql).toBe('\n  ALTER TABLE public.customers ADD COLUMN telepon text;  \n')
  })

  it('migration irreversible disimpan tanpa SQL down dan dengan alasan', async () => {
    const { user } = openAs('reviewer', page)
    await ready()
    await user.type(screen.getByLabelText('Judul'), 'Tambah kolom telepon')
    await setSql('SQL up', 'DROP TABLE public.promo;')
    await user.click(screen.getByLabelText('Irreversible (tidak dapat dibatalkan)'))
    await user.type(screen.getByLabelText('Alasan irreversible'), 'Tabel dihapus permanen')
    await save(user)
    await screen.findByRole('status')
    expect(created()[0]).toMatchObject({ irreversible: true, down_sql: null, irreversible_reason: 'Tabel dihapus permanen' })
  })

  it('migration batch disimpan dengan konfigurasi dan predikat kosong menjadi null', async () => {
    const { user } = openAs('reviewer', page)
    await ready()
    await user.type(screen.getByLabelText('Judul'), 'Tambah kolom telepon')
    await user.click(screen.getByLabelText('Batch'))
    await setSql('Bagian SET (up)', "status = 'arsip'")
    await setSql('Bagian SET (down)', "status = 'aktif'")
    await user.type(screen.getByLabelText('Tabel'), 'orders')
    await save(user)
    await screen.findByRole('status')
    expect(created()[0]).toMatchObject({ mode: 'batch', up_sql: "status = 'arsip'", batch: { schema: 'public', table: 'orders', key_column: 'id', batch_size: 1000, scope_where: null } })
  })

  it('formulir kosong ditolak di browser tanpa request dan fokus ke judul', async () => {
    const spy = interceptFetch('/migrations')
    const { user } = openAs('reviewer', page)
    await ready()
    await save(user)
    expect(screen.getByText('Judul wajib diisi')).toBeInTheDocument()
    expect(screen.getByText('SQL up wajib diisi')).toBeInTheDocument()
    expect(screen.getByText('SQL down wajib diisi atau tandai migration irreversible')).toBeInTheDocument()
    expect(screen.getByLabelText('Judul')).toHaveFocus()
    expect(spy.mock.calls.filter(([, init]) => init?.method === 'POST')).toHaveLength(0)
  })

  it('SQL yang ditolak server tampil di editornya dan tidak ada draft yang dibuat', async () => {
    const { user } = openAs('reviewer', page)
    await fillNormal(user)
    await setSql('SQL up', 'COMMIT;')
    await save(user)
    expect(await screen.findByText('Perintah transaksi diatur oleh aplikasi.')).toBeInTheDocument()
    expect(screen.getByRole('textbox', { name: 'SQL up' })).toHaveFocus()
    expect(created()).toHaveLength(0)
    expect(screen.getByLabelText('Judul')).toHaveValue('Tambah kolom telepon')
  })

  it('menyimpan dua kali dengan cepat hanya membuat satu draft', async () => {
    interceptFetch('/migrations', { delay: 100 })
    const { user } = openAs('reviewer', page)
    await fillNormal(user)
    const button = screen.getByRole('button', { name: 'Simpan draft' })
    // Submit form dikirim dua kali seperti menekan Enter berturut-turut karena tombol yang terkunci tidak menerima klik kedua
    const form = button.closest('form') as HTMLFormElement
    fireEvent.submit(form)
    fireEvent.submit(form)
    await screen.findByRole('status')
    expect(created()).toHaveLength(1)
  })

  it('tombol Kembali menuju daftar migration', async () => {
    openAs('reviewer', page)
    await ready()
    expect(screen.getByRole('link', { name: 'Kembali' })).toHaveAttribute('href', `/databases/${ids.inventaris}/migrations`)
  })
})

describe('isian awal', () => {
  it('berasal dari schema diff dengan catatan agar SQL down diisi', async () => {
    const form = { ...emptyForm(), title: 'Samakan schema public', upSql: 'ALTER TABLE public.t ADD COLUMN a int;' }
    openAs('reviewer', { pathname: page, state: { prefill: { form, source: 'diff' } } })
    expect(await screen.findByText(/SQL up berasal dari schema diff/)).toBeInTheDocument()
    await ready()
    expect(screen.getByLabelText('Judul')).toHaveValue('Samakan schema public')
    expect(await sqlOf('SQL up')).toBe('ALTER TABLE public.t ADD COLUMN a int;')
    expect(await sqlOf('SQL down')).toBe('')
  })

  it('state yang bentuknya salah diabaikan dan form tetap kosong', async () => {
    openAs('reviewer', { pathname: page, state: { prefill: { form: { title: 5 }, source: 'diff' } } })
    await ready()
    expect(screen.getByLabelText('Judul')).toHaveValue('')
    expect(screen.queryByText(/berasal dari schema diff/)).not.toBeInTheDocument()
  })
})
