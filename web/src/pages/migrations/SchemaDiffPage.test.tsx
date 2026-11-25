// @vitest-environment jsdom
import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { handlers } from '../../mocks/handlers/index.ts'
import { ids } from '../../mocks/ids.ts'
import { getState } from '../../mocks/state.ts'
import { setupMockServer } from '../../mocks/testing.ts'
import { openAs } from '../../test/app.tsx'
import { setSql, sqlOf } from '../../test/editor.ts'
import { interceptFetch } from '../../test/network.ts'

setupMockServer(...handlers)

const page = (db: string, search = '') => `/databases/${db}/schema-diff${search}`
const inventaris = page(ids.inventaris)
type User = ReturnType<typeof openAs>['user']

async function compare(user: User, desired: string, { current = 'public', target = 'public' } = {}) {
  await screen.findByRole('option', { name: 'pelaporan' })
  await user.clear(screen.getByLabelText('Schema saat ini'))
  await user.type(screen.getByLabelText('Schema saat ini'), current)
  await user.clear(screen.getByLabelText('Schema pembanding'))
  await user.type(screen.getByLabelText('Schema pembanding'), target)
  await user.selectOptions(screen.getByLabelText('Database pembanding'), desired)
  await user.click(screen.getByRole('button', { name: 'Bandingkan' }))
}

const stat = (label: string) => (screen.getByText(label, { selector: 'p.detail-subtitle' }).closest('.detail') as HTMLElement).querySelector('.number')!.textContent

describe('form perbandingan', () => {
  it('menampilkan isian schema bawaan dan hanya database yang mendukung schema diff', async () => {
    openAs('reviewer', inventaris)
    await screen.findByRole('option', { name: 'pelaporan' })
    expect(screen.getByLabelText('Schema saat ini')).toHaveValue('public')
    expect(screen.getByLabelText('Schema pembanding')).toHaveValue('public')
    const options = within(screen.getByLabelText('Database pembanding')).getAllByRole('option').map((option) => option.textContent)
    expect(options).toEqual(['Pilih database', 'inventaris (database ini)', 'pelaporan'])
  })

  it('database MySQL tidak ditawarkan sebagai pembanding', async () => {
    openAs('observer', inventaris)
    await screen.findByRole('option', { name: 'inventaris (database ini)' })
    expect(screen.queryByRole('option', { name: 'arsip' })).not.toBeInTheDocument()
  })

  it('formulir yang belum lengkap ditolak di browser tanpa request', async () => {
    const spy = interceptFetch('/schema-diff')
    const { user } = openAs('reviewer', inventaris)
    await screen.findByRole('option', { name: 'pelaporan' })
    await user.clear(screen.getByLabelText('Schema saat ini'))
    await user.click(screen.getByRole('button', { name: 'Bandingkan' }))
    expect(screen.getByText('Schema saat ini wajib diisi')).toBeInTheDocument()
    expect(screen.getByText('Pilih database pembanding')).toBeInTheDocument()
    expect(screen.getByLabelText('Schema saat ini')).toHaveFocus()
    expect(spy.mock.calls.filter(([input]) => String(input).includes('/schema-diff'))).toHaveLength(0)
  })

  it('nama schema yang terlalu panjang ditolak', async () => {
    const { user } = openAs('reviewer', inventaris)
    await screen.findByRole('option', { name: 'pelaporan' })
    await user.clear(screen.getByLabelText('Schema pembanding'))
    await user.type(screen.getByLabelText('Schema pembanding'), 's'.repeat(64))
    await user.click(screen.getByRole('button', { name: 'Bandingkan' }))
    expect(screen.getByText('Schema pembanding maksimal 63 byte')).toBeInTheDocument()
  })
})

describe('hasil perbandingan', () => {
  it('membandingkan dengan database lain menampilkan ringkasan, tabel perbedaan, dan SQL kandidat', async () => {
    const { user } = openAs('reviewer', inventaris)
    await compare(user, 'pelaporan')
    const changes = await screen.findByRole('table', { name: 'Perbedaan schema' })
    const rows = within(changes).getAllByRole('row').slice(1)
    expect(rows.length).toBeGreaterThan(0)
    expect(Number(stat('Perubahan'))).toBe(rows.length)
    expect(Number(stat('Tambah')) + Number(stat('Ubah')) + Number(stat('Hapus'))).toBe(rows.length)
    expect(await screen.findByRole('textbox', { name: 'SQL up kandidat' })).toHaveAttribute('aria-readonly', 'true')
    expect(screen.getByRole('button', { name: 'Buat draft migration dari diff' })).toBeInTheDocument()
  })

  it('hasil tersimpan di URL sehingga tautannya bisa dibagikan dan langsung dijalankan', async () => {
    openAs('reviewer', page(ids.inventaris, `?desired=${ids.pelaporan}&current_schema=public&desired_schema=public`))
    expect(await screen.findByRole('table', { name: 'Perbedaan schema' })).toBeInTheDocument()
    expect(screen.getByLabelText('Database pembanding')).toHaveValue(ids.pelaporan)
  })

  it('membandingkan dengan dirinya sendiri menyatakan tidak ada perbedaan', async () => {
    const { user } = openAs('reviewer', inventaris)
    await compare(user, 'inventaris (database ini)')
    expect(await screen.findByText(/Tidak ada perbedaan/)).toBeInTheDocument()
    expect(screen.queryByRole('table', { name: 'Perbedaan schema' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Buat draft migration dari diff' })).not.toBeInTheDocument()
  })

  it('schema yang tidak ditemukan ditandai pada isian yang bersangkutan', async () => {
    const { user } = openAs('reviewer', inventaris)
    await compare(user, 'pelaporan', { current: 'tidak_ada' })
    expect(await screen.findByText('Schema tidak ditemukan')).toBeInTheDocument()
    expect(screen.getByLabelText('Schema saat ini')).toHaveAttribute('aria-invalid', 'true')
    expect(screen.queryByRole('table', { name: 'Perbedaan schema' })).not.toBeInTheDocument()
  })

  it('database pembanding yang tidak boleh diakses menampilkan halaman akses ditolak', async () => {
    openAs('reviewer', page(ids.inventaris, `?desired=${ids.arsip}`))
    expect(await screen.findByText('Akses ditolak')).toBeInTheDocument()
  })

  it('perbandingan menampilkan penanda placeholder selama memuat', async () => {
    interceptFetch('/schema-diff', { delay: 300 })
    openAs('reviewer', page(ids.inventaris, `?desired=${ids.pelaporan}`))
    expect(await screen.findByText('Memuat…')).toBeInTheDocument()
    expect(await screen.findByRole('table', { name: 'Perbedaan schema' })).toBeInTheDocument()
  })

  it('perubahan yang menghilangkan data diberi penanda peringatan', async () => {
    const { user } = openAs('reviewer', page(ids.pelaporan))
    await screen.findByRole('option', { name: 'inventaris' })
    await user.selectOptions(screen.getByLabelText('Database pembanding'), 'inventaris')
    await user.click(screen.getByRole('button', { name: 'Bandingkan' }))
    const changes = await screen.findByRole('table', { name: 'Perbedaan schema' })
    const hasDrop = within(changes).queryAllByText('Hapus').length > 0
    expect(within(changes).queryAllByRole('img', { name: 'Data bisa hilang' }).length > 0).toBe(hasDrop)
    expect(Number(stat('Hapus'))).toBe(within(changes).queryAllByText('Hapus').length)
  })

  it('pengguna dengan akses lihat saja melihat hasil tanpa tombol membuat draft', async () => {
    getState().grants.push({ group_id: ids.readers, database_id: ids.pelaporan, access: 'view_only' })
    const { user } = openAs('observer', inventaris)
    await screen.findByRole('option', { name: 'pelaporan' })
    await user.selectOptions(screen.getByLabelText('Database pembanding'), 'pelaporan')
    await user.click(screen.getByRole('button', { name: 'Bandingkan' }))
    expect(await screen.findByRole('table', { name: 'Perbedaan schema' })).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Buat draft migration dari diff' })).not.toBeInTheDocument()
    expect(screen.getByText(/tidak punya akses membuat migration/)).toBeInTheDocument()
  })
})

describe('membuat draft dari diff', () => {
  it('membawa SQL kandidat ke editor baru lalu draft bisa disimpan setelah SQL down diisi', async () => {
    const { user } = openAs('reviewer', inventaris)
    await compare(user, 'pelaporan')
    const candidate = await sqlOf('SQL up kandidat')
    await user.click(screen.getByRole('button', { name: 'Buat draft migration dari diff' }))
    expect(await screen.findByRole('heading', { name: 'Draft baru' })).toBeInTheDocument()
    expect(screen.getByText(/SQL up berasal dari schema diff/)).toBeInTheDocument()
    expect(screen.getByLabelText('Judul')).toHaveValue('Samakan schema public dengan pelaporan.public')
    await screen.findByRole('textbox', { name: 'SQL up' })
    expect(await sqlOf('SQL up')).toBe(candidate)
    expect(await sqlOf('SQL down')).toBe('')
    await user.click(screen.getByRole('button', { name: 'Simpan draft' }))
    expect(screen.getByText('SQL down wajib diisi atau tandai migration irreversible')).toBeInTheDocument()
    await setSql('SQL down', 'SELECT 1;')
    await user.click(screen.getByRole('button', { name: 'Simpan draft' }))
    expect(await screen.findByRole('status')).toHaveTextContent('Draft berhasil dibuat.')
    expect(getState().migrations.some((migration) => migration.title === 'Samakan schema public dengan pelaporan.public' && migration.up_sql === candidate)).toBe(true)
  })
})
