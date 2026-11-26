// @vitest-environment jsdom
import { screen, within } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { handlers } from '../../mocks/handlers/index.ts'
import { ids, workflowIds } from '../../mocks/ids.ts'
import { getState } from '../../mocks/state.ts'
import { setupMockServer } from '../../mocks/testing.ts'
import { openAs } from '../../test/app.tsx'
import { realServerErrors } from '../../test/network.ts'

setupMockServer(...handlers)

const sql = ["SELECT * FROM public.orders WHERE id = 1;", "UPDATE public.orders SET status = 'x' WHERE id = 1;", 'CREATE INDEX idx_x ON public.orders (id);'].join('\n')

// Draft dengan tiga jenis statement ditambahkan langsung ke state mock supaya tiap perilaku EXPLAIN bisa diuji
function seedMigration(patch: Record<string, unknown> = {}) {
  const base = getState().migrations.find((migration) => migration.id === workflowIds.migrationD)!
  const migration = { ...base, id: crypto.randomUUID(), title: 'Analisis plan', up_sql: sql, down_sql: 'SELECT 1;', ...patch }
  getState().migrations.push(migration)
  return migration
}

const open = (id: string, user = 'reviewer') => openAs(user, `/databases/${ids.inventaris}/migrations/${id}`)
const panel = async () => within((await screen.findByRole('heading', { name: 'EXPLAIN' })).closest('.card') as HTMLElement)
const run = (user: ReturnType<typeof openAs>['user']) => user.click(screen.getByRole('button', { name: 'Jalankan EXPLAIN' }))

describe('panel EXPLAIN', () => {
  it('menampilkan statement tersimpan dengan nomor dan pratinjau', async () => {
    open(seedMigration().id)
    const explain = await panel()
    const options = within(explain.getByLabelText('Statement')).getAllByRole('option').map((option) => option.textContent)
    expect(options).toEqual(['1. SELECT * FROM public.orders WHERE id = 1', "2. UPDATE public.orders SET status = 'x' WHERE id = 1", '3. CREATE INDEX idx_x ON public.orders (id)'])
  })

  it('menjelaskan SELECT tanpa ANALYZE dengan node plan dan waktu yang tidak tersedia', async () => {
    const { user } = open(seedMigration().id)
    await panel()
    await run(user)
    const nodes = await screen.findByRole('table', { name: 'Node plan statement 1' })
    expect(within(nodes).getByText('Index Scan')).toBeInTheDocument()
    expect(within(nodes).getByText('orders_pkey')).toBeInTheDocument()
    const facts = screen.getByText('Planning time (ms)').closest('dl') as HTMLElement
    expect(within(facts).getByText('Planning time (ms)').nextElementSibling).toHaveTextContent('-')
    expect(within(facts).getByText('Execution time (ms)').nextElementSibling).toHaveTextContent('-')
    expect(within(facts).getByText('Revision').nextElementSibling).toHaveTextContent('1')
  })

  it('ANALYZE pada SELECT menampilkan waktu planning dan eksekusi', async () => {
    const { user } = open(seedMigration().id)
    await panel()
    await user.click(screen.getByLabelText('Sertakan ANALYZE'))
    await run(user)
    await screen.findByRole('table', { name: 'Node plan statement 1' })
    const facts = screen.getByText('Planning time (ms)').closest('dl') as HTMLElement
    expect(within(facts).getByText('Planning time (ms)').nextElementSibling).not.toHaveTextContent('-')
    expect(within(facts).getByText('Execution time (ms)').nextElementSibling).not.toHaveTextContent('-')
    expect(within(facts).getByText('Statement').nextElementSibling).toHaveTextContent('dengan ANALYZE')
  })

  it('UPDATE dijelaskan sebagai node bertingkat yang diratakan dengan penanda indentasi', async () => {
    const { user } = open(seedMigration().id)
    await panel()
    await user.selectOptions(screen.getByLabelText('Statement'), '2')
    await run(user)
    const nodes = await screen.findByRole('table', { name: 'Node plan statement 2' })
    const rows = within(nodes).getAllByRole('row').slice(1)
    expect(rows).toHaveLength(2)
    expect(rows[0]).toHaveTextContent('ModifyTable')
    expect(rows[1]).toHaveTextContent('↳ Index Scan')
  })

  it('ANALYZE pada statement yang mengubah data ditolak dengan pesan di dekat kotak centang', async () => {
    const { user } = open(seedMigration().id)
    await panel()
    await user.selectOptions(screen.getByLabelText('Statement'), '2')
    await user.click(screen.getByLabelText('Sertakan ANALYZE'))
    await run(user)
    expect(await screen.findByText('ANALYZE hanya untuk SELECT yang tidak mengubah data')).toBeInTheDocument()
    expect(screen.getByLabelText('Sertakan ANALYZE')).toHaveAttribute('aria-invalid', 'true')
    expect(screen.queryByRole('table', { name: /Node plan/ })).not.toBeInTheDocument()
  })

  it('statement yang tidak bisa dijelaskan ditolak dengan pesan pada pilihan statement', async () => {
    const { user } = open(seedMigration().id)
    await panel()
    await user.selectOptions(screen.getByLabelText('Statement'), '3')
    await run(user)
    expect(await screen.findByText('Jenis statement ini tidak dapat dijelaskan')).toBeInTheDocument()
    expect(screen.getByLabelText('Statement')).toHaveAttribute('aria-invalid', 'true')
  })

  it('mengganti statement atau arah menghapus hasil sebelumnya', async () => {
    const { user } = open(seedMigration().id)
    await panel()
    await run(user)
    await screen.findByRole('table', { name: 'Node plan statement 1' })
    await user.selectOptions(screen.getByLabelText('Statement'), '2')
    expect(screen.queryByRole('table', { name: /Node plan/ })).not.toBeInTheDocument()
  })

  it('arah down memakai statement dari SQL down', async () => {
    const { user } = open(seedMigration({ down_sql: 'SELECT 1;\nSELECT 2;' }).id)
    await panel()
    await user.selectOptions(screen.getByLabelText('Arah'), 'down')
    const options = within(screen.getByLabelText('Statement')).getAllByRole('option').map((option) => option.textContent)
    expect(options).toEqual(['1. SELECT 1', '2. SELECT 2'])
    await user.selectOptions(screen.getByLabelText('Statement'), '2')
    await run(user)
    expect(await screen.findByRole('table', { name: 'Node plan statement 2' })).toBeInTheDocument()
  })

  it('perubahan yang belum disimpan mengunci tombol EXPLAIN dengan penjelasan', async () => {
    const { user } = open(seedMigration().id)
    await panel()
    expect(screen.getByRole('button', { name: 'Jalankan EXPLAIN' })).toBeEnabled()
    await user.type(screen.getByLabelText('Judul'), ' baru')
    expect(screen.getByRole('button', { name: 'Jalankan EXPLAIN' })).toBeDisabled()
    expect(screen.getByText(/Simpan draft dulu karena analisis memakai revision yang tersimpan/)).toBeInTheDocument()
  })

  it('migration batch hanya menampilkan keterangan bahwa EXPLAIN tidak tersedia', async () => {
    open(workflowIds.migrationC)
    const explain = await panel()
    expect(explain.getByText('EXPLAIN hanya tersedia untuk migration mode normal.')).toBeInTheDocument()
    expect(explain.queryByRole('button', { name: 'Jalankan EXPLAIN' })).not.toBeInTheDocument()
  })

  it('migration irreversible tidak menawarkan arah down', async () => {
    open(workflowIds.migrationE)
    const explain = await panel()
    expect(within(explain.getByLabelText('Arah')).getByRole('option', { name: 'SQL down' })).toBeDisabled()
  })

  it('revision yang berubah di server ditolak dengan petunjuk memuat ulang', async () => {
    const migration = seedMigration()
    const { user } = open(migration.id)
    await panel()
    migration.revision = 7
    await run(user)
    expect(await screen.findByRole('alert')).toHaveTextContent('Migration sudah diubah')
    expect(screen.getByRole('alert')).toHaveTextContent('Muat ulang halaman ini')
  })

  it('konflik dari server sungguhan tanpa reason tetap memberi petunjuk memuat ulang', async () => {
    const migration = seedMigration()
    realServerErrors()
    const { user } = open(migration.id)
    await panel()
    migration.revision = 7
    await run(user)
    expect(await screen.findByRole('alert')).toHaveTextContent('Data atau status berubah')
    expect(screen.getByRole('alert')).toHaveTextContent('Muat ulang halaman ini')
  })

  it('JSON plan terstruktur tersedia dalam bagian yang bisa dibuka', async () => {
    const { user } = open(seedMigration().id)
    await panel()
    await run(user)
    await screen.findByRole('table', { name: 'Node plan statement 1' })
    const summary = screen.getByText('JSON plan terstruktur')
    expect(summary.closest('details')).not.toHaveAttribute('open')
    await user.click(summary)
    expect(summary.closest('details')?.querySelector('pre')?.textContent).toContain('"node_type": "Index Scan"')
  })
})
