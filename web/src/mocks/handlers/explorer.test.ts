import { describe, expect, it } from 'vitest'
import type { Schemas } from '../../api/types.ts'
import { ids } from '../seed.ts'
import { getState } from '../state.ts'
import { failureOf, newClient, setupMockServer, signIn } from '../testing.ts'
import { adminHandlers } from './admin.ts'
import { authHandlers } from './auth.ts'
import { explorerHandlers } from './explorer.ts'

setupMockServer(...authHandlers, ...adminHandlers, ...explorerHandlers)

type Page<T> = { items: T[]; page: number; limit: number; has_next: boolean }

const ordersRows = `/databases/${ids.inventaris}/tables/public/orders/rows`

describe('GET /databases', () => {
  it('menolak request tanpa sesi', async () => {
    expect((await failureOf(newClient().get('/databases', { handleUnauthenticated: false }))).status).toBe(401)
  })

  it('hanya menampilkan database dengan akses efektif, terurut menurut nama', async () => {
    const { client } = await signIn('reviewer')
    const page = await client.get<Page<Schemas['Database']>>('/databases')
    expect(page.items.map((database) => [database.name, database.access])).toEqual([
      ['inventaris', 'view_edit'],
      ['pelaporan', 'view_edit'],
    ])
    const observer = await signIn('observer')
    const readonly = await observer.client.get<Page<Schemas['Database']>>('/databases')
    expect(readonly.items.map((database) => [database.name, database.access])).toEqual([
      ['arsip', 'view_only'],
      ['inventaris', 'view_only'],
    ])
  })

  it('kapabilitas mengikuti DBMS dan MySQL tidak mendukung fitur lanjutan', async () => {
    const { client } = await signIn('observer')
    const page = await client.get<Page<Schemas['Database']>>('/databases')
    const arsip = page.items.find((database) => database.name === 'arsip')
    const inventaris = page.items.find((database) => database.name === 'inventaris')
    expect(arsip).toMatchObject({ engine: 'mysql', capabilities: { schema_diff: false, query_plans: false, workflow: false, batch: false } })
    expect(inventaris).toMatchObject({ engine: 'postgresql', capabilities: { schema_diff: true, query_plans: true, workflow: true, batch: true } })
  })

  it('platform admin tidak otomatis punya akses database', async () => {
    const { client } = await signIn('admin')
    expect(await client.get('/databases')).toEqual({ items: [], page: 1, limit: 50, has_next: false })
  })

  it('mengikuti perubahan akses dari panel admin', async () => {
    const admin = await signIn('admin')
    await admin.client.delete(`/admin/grants/${ids.readers}/${ids.arsip}`)
    const { client } = await signIn('observer')
    const page = await client.get<Page<Schemas['Database']>>('/databases')
    expect(page.items.map((database) => database.name)).toEqual(['inventaris'])
  })

  it('dipaginasi dan menolak query asing', async () => {
    const { client } = await signIn('reviewer')
    const first = await client.get<Page<Schemas['Database']>>('/databases', { query: { limit: 1 } })
    expect(first.items).toHaveLength(1)
    expect(first.has_next).toBe(true)
    expect((await failureOf(client.get('/databases', { query: { q: 'inv' } }))).code).toBe('bad_request')
  })
})

describe('GET /databases/{id}/schema', () => {
  it('mengelompokkan tabel per schema dengan jenisnya', async () => {
    const { client } = await signIn('reviewer')
    const catalog = await client.get<Schemas['SchemaCatalog']>(`/databases/${ids.inventaris}/schema`)
    expect(catalog.schemas.map((schema) => schema.name)).toEqual(['audit', 'public'])
    const publicSchema = catalog.schemas.find((schema) => schema.name === 'public')
    expect(publicSchema?.tables).toEqual([
      { name: 'customers', kind: 'table' },
      { name: 'orders', kind: 'table' },
      { name: 'products', kind: 'table' },
      { name: 'v_stok_menipis', kind: 'view' },
    ])
  })

  it('view_only juga boleh membaca', async () => {
    const { client } = await signIn('observer')
    await expect(client.get(`/databases/${ids.inventaris}/schema`)).resolves.toBeTruthy()
  })

  it('tanpa akses menghasilkan 403, termasuk untuk database yang tidak ada', async () => {
    const { client } = await signIn('observer')
    expect((await failureOf(client.get(`/databases/${ids.pelaporan}/schema`))).code).toBe('forbidden')
    expect((await failureOf(client.get('/databases/10000000-0000-4000-8000-000000000099/schema'))).code).toBe('forbidden')
    const admin = await signIn('admin')
    expect((await failureOf(admin.client.get(`/databases/${ids.inventaris}/schema`))).code).toBe('forbidden')
  })

  it('menolak ID yang bukan UUID', async () => {
    const { client } = await signIn('reviewer')
    expect((await failureOf(client.get('/databases/bukan-uuid/schema'))).code).toBe('bad_request')
  })

  it('database baru tanpa tabel menghasilkan katalog kosong', async () => {
    const admin = await signIn('admin')
    const created = await admin.client.post<Schemas['Connection']>('/admin/connections', { name: 'kosong', engine: 'postgresql', dsn: 'postgres://u:p@h/db' })
    await admin.client.put(`/admin/grants/${ids.writers}/${created.id}`, { access: 'view_only' })
    const { client } = await signIn('reviewer')
    await expect(client.get(`/databases/${created.id}/schema`)).resolves.toEqual({ schemas: [] })
  })
})

describe('GET /databases/{id}/tables/{schema}/{table}', () => {
  it('membawa kolom, constraint, dan index', async () => {
    const { client } = await signIn('reviewer')
    const table = await client.get<Schemas['Table']>(`/databases/${ids.inventaris}/tables/public/orders`)
    expect(table).toMatchObject({ schema: 'public', name: 'orders', kind: 'table' })
    expect(table.columns.map((column) => column.name)).toEqual(['id', 'customer_id', 'product_id', 'jumlah', 'total', 'status', 'dibuat'])
    expect(table.columns[5]).toEqual({ name: 'status', data_type: 'text', nullable: false, default: "'baru'::text" })
    expect(table.constraints.map((constraint) => constraint.type)).toEqual(['PRIMARY KEY', 'FOREIGN KEY', 'FOREIGN KEY', 'CHECK'])
    expect(table.indexes.map((index) => [index.name, index.primary])).toEqual([
      ['orders_pkey', true],
      ['idx_orders_customer', false],
    ])
  })

  it('view tidak punya constraint atau index', async () => {
    const { client } = await signIn('reviewer')
    const view = await client.get<Schemas['Table']>(`/databases/${ids.inventaris}/tables/public/v_stok_menipis`)
    expect(view).toMatchObject({ kind: 'view', constraints: [], indexes: [] })
  })

  it('tabel atau schema yang tidak ada menghasilkan 404', async () => {
    const { client } = await signIn('reviewer')
    expect((await failureOf(client.get(`/databases/${ids.inventaris}/tables/public/tidak_ada`))).code).toBe('not_found')
    expect((await failureOf(client.get(`/databases/${ids.inventaris}/tables/lain/orders`))).code).toBe('not_found')
    expect((await failureOf(client.get(`/databases/${ids.inventaris}/tables/arsip/dokumen`))).code).toBe('not_found')
  })

  it('identifier yang di-encode dibaca apa adanya dan identifier terlalu panjang ditolak', async () => {
    getState().tables.push({ ...getState().tables[0], schema: 'sch ema', name: 'tabel/unik?1' })
    const { client } = await signIn('reviewer')
    const table = await client.get<Schemas['Table']>(`/databases/${ids.inventaris}/tables/${encodeURIComponent('sch ema')}/${encodeURIComponent('tabel/unik?1')}`)
    expect(table).toMatchObject({ schema: 'sch ema', name: 'tabel/unik?1' })
    expect((await failureOf(client.get(`/databases/${ids.inventaris}/tables/public/${'t'.repeat(64)}`))).code).toBe('bad_request')
  })
})

describe('GET /databases/{id}/tables/{schema}/{table}/rows', () => {
  it('menampilkan 50 baris pertama dengan nama kolom dan urutan primary key', async () => {
    const { client } = await signIn('reviewer')
    const page = await client.get<Schemas['RowsPage']>(ordersRows)
    expect(page).toMatchObject({ page: 1, limit: 50, has_next: true, order: 'primary_key' })
    expect(page.columns).toEqual(['id', 'customer_id', 'product_id', 'jumlah', 'total', 'status', 'dibuat'])
    expect(page.items).toHaveLength(50)
    expect(page.items[0][0]).toEqual({ text: '1', null: false, truncated: false, encoding: 'text' })
  })

  it('halaman terakhir memuat sisa baris dan tidak punya halaman berikutnya', async () => {
    const { client } = await signIn('reviewer')
    const page = await client.get<Schemas['RowsPage']>(ordersRows, { query: { page: 3 } })
    expect(page.items).toHaveLength(37)
    expect(page.has_next).toBe(false)
    expect(page.items[36][0].text).toBe('137')
    const beyond = await client.get<Schemas['RowsPage']>(ordersRows, { query: { page: 9 } })
    expect(beyond.items).toEqual([])
  })

  it('limit diterima sampai 200 dan di atasnya ditolak', async () => {
    const { client } = await signIn('reviewer')
    const all = await client.get<Schemas['RowsPage']>(ordersRows, { query: { limit: 200 } })
    expect(all.items).toHaveLength(137)
    expect(all.has_next).toBe(false)
    expect((await failureOf(client.get(ordersRows, { query: { limit: 201 } }))).code).toBe('bad_request')
    expect((await failureOf(client.get(ordersRows, { query: { sort: 'id' } }))).code).toBe('bad_request')
  })

  it('NULL ditandai terpisah dari teks kosong', async () => {
    const { client } = await signIn('reviewer')
    const page = await client.get<Schemas['RowsPage']>(`/databases/${ids.inventaris}/tables/public/customers/rows`)
    const email = page.columns.indexOf('email')
    expect(page.items[4][email]).toEqual({ text: '', null: true, truncated: false, encoding: 'text' })
    expect(page.items[0][email].null).toBe(false)
  })

  it('tabel tanpa primary key berurutan best effort, bytea berupa hex, dan teks panjang dipotong', async () => {
    const { client } = await signIn('reviewer')
    const page = await client.get<Schemas['RowsPage']>(`/databases/${ids.inventaris}/tables/audit/log_entries/rows`)
    expect(page.order).toBe('best_effort')
    const signature = page.columns.indexOf('tanda_tangan')
    expect(page.items[0][signature]).toMatchObject({ encoding: 'hex', text: '\\x00000001' })
    const detail = page.columns.indexOf('detail')
    expect(page.items[2][detail]).toMatchObject({ truncated: true, null: false })
    expect(page.items[2][detail].text).toHaveLength(4096)
    expect(page.items[3][detail].null).toBe(true)
  })

  it('view dan tabel MySQL juga bisa dilihat isinya', async () => {
    const reviewer = await signIn('reviewer')
    const view = await reviewer.client.get<Schemas['RowsPage']>(`/databases/${ids.inventaris}/tables/public/v_stok_menipis/rows`)
    expect(view.columns).toEqual(['sku', 'nama', 'stok'])
    expect(view.items.every((row) => Number(row[2].text) < 10)).toBe(true)
    const observer = await signIn('observer')
    const documents = await observer.client.get<Schemas['RowsPage']>(`/databases/${ids.arsip}/tables/arsip/dokumen/rows`, { query: { limit: 5 } })
    expect(documents.items).toHaveLength(5)
    expect(documents.has_next).toBe(true)
  })

  it('tanpa akses ke database tidak bisa membaca isi tabelnya', async () => {
    const { client } = await signIn('auditor')
    expect((await failureOf(client.get(ordersRows))).code).toBe('forbidden')
  })
})
