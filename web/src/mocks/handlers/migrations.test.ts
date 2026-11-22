import { describe, expect, it } from 'vitest'
import type { Schemas } from '../../api/types.ts'
import { definitionChecksum } from '../checksum.ts'
import { ids, workflowIds as w } from '../ids.ts'
import { getState } from '../state.ts'
import { failureOf, setupMockServer, signIn } from '../testing.ts'
import { authHandlers } from './auth.ts'
import { migrationHandlers } from './migrations.ts'

setupMockServer(...authHandlers, ...migrationHandlers)

type Page<T> = { items: T[]; page: number; limit: number; has_next: boolean }

const base = `/databases/${ids.inventaris}/migrations`
const definition = { title: 'Tambah kolom warna', up_sql: 'ALTER TABLE public.products ADD COLUMN warna text;', down_sql: 'ALTER TABLE public.products DROP COLUMN warna;', irreversible: false, irreversible_reason: '', mode: 'normal', batch: null }
const explainable = { ...definition, title: 'Uji plan', up_sql: "SELECT * FROM public.orders WHERE id = 1; UPDATE public.orders SET status = 'x' WHERE status = 'baru'; CREATE INDEX i ON public.orders (status);", down_sql: 'SELECT 1;' }

async function create(user: string, body: unknown = definition) {
  const { client } = await signIn(user)
  return { client, migration: await client.post<Schemas['Migration']>(base, body) }
}

describe('GET migrations', () => {
  it('daftar terbaru lebih dulu dan tanpa badan SQL', async () => {
    const { client } = await signIn('reviewer')
    const page = await client.get<Page<Schemas['MigrationSummary']>>(base)
    expect(page.items.map((item) => item.id)).toEqual([w.migrationE, w.migrationD, w.migrationC, w.migrationB, w.migrationA])
    expect(page.items[0]).toMatchObject({ title: 'Hapus kolom promo lama', irreversible: true, mode: 'normal', state: 'draft', revision: 1, first_execution_at: null })
    expect(page.items[2]).toMatchObject({ mode: 'batch', state: 'frozen', first_execution_at: '2026-11-10T10:05:01Z' })
    expect(JSON.stringify(page)).not.toMatch(/up_sql|down_sql|ALTER TABLE|batch_size/)
  })

  it('dipaginasi dan view_only boleh membaca', async () => {
    const { client } = await signIn('observer')
    const page = await client.get<Page<Schemas['MigrationSummary']>>(base, { query: { limit: 2, page: 2 } })
    expect(page.items.map((item) => item.id)).toEqual([w.migrationC, w.migrationB])
    expect(page.has_next).toBe(true)
  })

  it('tanpa akses 403, dan database MySQL tidak mendukung workflow', async () => {
    const auditor = await signIn('auditor')
    expect((await failureOf(auditor.client.get(base))).code).toBe('forbidden')
    const observer = await signIn('observer')
    const err = await failureOf(observer.client.get(`/databases/${ids.arsip}/migrations`))
    expect(err.status).toBe(422)
  })

  it('database lain hanya memuat migration miliknya', async () => {
    const { client } = await signIn('reviewer')
    const page = await client.get<Page<Schemas['MigrationSummary']>>(`/databases/${ids.pelaporan}/migrations`)
    expect(page.items.map((item) => item.id)).toEqual([w.migrationP])
  })
})

describe('POST migrations', () => {
  it('membuat draft revision 1 dengan checksum dari definisinya', async () => {
    const { client, migration } = await create('editor')
    expect(migration).toMatchObject({ ...definition, database_id: ids.inventaris, state: 'draft', revision: 1, created_by: ids.editor, first_execution_at: null })
    expect(migration.checksum).toBe(definitionChecksum(definition as Parameters<typeof definitionChecksum>[0]))
    expect(migration.created_at).toMatch(/^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$/)
    await expect(client.get(`${base}/${migration.id}`)).resolves.toMatchObject({ id: migration.id })
  })

  it('mengirim Location ke migration baru', async () => {
    const { me } = await signIn('editor')
    const res = await fetch(`http://localhost/api/v1${base}`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': me.csrf_token }, body: JSON.stringify(definition) })
    expect(res.status).toBe(201)
    expect(res.headers.get('Location')).toMatch(new RegExp(`^/api/v1${base}/[0-9a-f-]{36}$`))
  })

  it('view_only dan user tanpa akses ditolak tanpa membuat data', async () => {
    const before = getState().migrations.length
    const observer = await signIn('observer')
    expect((await failureOf(observer.client.post(base, definition))).code).toBe('forbidden')
    const auditor = await signIn('auditor')
    expect((await failureOf(auditor.client.post(base, definition))).code).toBe('forbidden')
    expect(getState().migrations).toHaveLength(before)
  })

  it('validasi menandai SQL up dan down per editor serta konfigurasi batch per path', async () => {
    const { client } = await signIn('editor')
    const sql = await failureOf(client.post(base, { ...definition, up_sql: 'BEGIN;', down_sql: 'COMMIT;' }))
    expect(sql.code).toBe('validation_failed')
    expect(Object.keys(sql.fields).sort()).toEqual(['down_sql', 'up_sql'])
    const batch = await failureOf(client.post(base, { ...definition, mode: 'batch', up_sql: "UPDATE t SET a = 1", down_sql: 'a = 0', batch: { schema: 'public', table: 'bad name', key_column: 'id', batch_size: 5, scope_where: null } }))
    expect(Object.keys(batch.fields).sort()).toEqual(['batch.batch_size', 'batch.table', 'up_sql'])
    const irreversible = await failureOf(client.post(base, { ...definition, down_sql: null, irreversible: true, irreversible_reason: '' }))
    expect(irreversible.fields.irreversible_reason).toHaveLength(1)
    expect((await failureOf(client.post(base, { ...definition, down_sql: null }))).fields.down_sql).toHaveLength(1)
  })

  it('menerima migration irreversible dengan alasan dan mode batch yang benar', async () => {
    const irreversible = await create('editor', { ...definition, down_sql: null, irreversible: true, irreversible_reason: 'Data hilang' })
    expect(irreversible.migration).toMatchObject({ irreversible: true, down_sql: null, irreversible_reason: 'Data hilang' })
    const batch = { schema: 'public', table: 'orders', key_column: 'id', batch_size: 100, scope_where: "status = 'selesai'" }
    const created = await create('editor', { ...definition, mode: 'batch', up_sql: "status = 'arsip'", down_sql: "status = 'selesai'", batch })
    expect(created.migration).toMatchObject({ mode: 'batch', batch })
  })

  it('menolak field asing, tipe salah, dan Content-Type selain JSON', async () => {
    const { client, me } = await signIn('editor')
    expect((await failureOf(client.post(base, { ...definition, extra: 1 }))).code).toBe('bad_request')
    expect((await failureOf(client.post(base, { ...definition, irreversible: 'ya' }))).code).toBe('bad_request')
    const res = await fetch(`http://localhost/api/v1${base}`, { method: 'POST', headers: { 'Content-Type': 'text/plain', 'X-CSRF-Token': me.csrf_token }, body: '{}' })
    expect(res.status).toBe(415)
  })
})

describe('GET migration', () => {
  it('membawa SQL lengkap, dan ID yang salah database atau tidak ada adalah 404', async () => {
    const { client } = await signIn('reviewer')
    const migration = await client.get<Schemas['Migration']>(`${base}/${w.migrationB}`)
    expect(migration).toMatchObject({ id: w.migrationB, state: 'frozen', up_sql: expect.stringContaining('ADD COLUMN diskon'), down_sql: expect.stringContaining('DROP COLUMN') })
    expect((await failureOf(client.get(`${base}/${w.migrationP}`))).code).toBe('not_found')
    expect((await failureOf(client.get(`${base}/20000000-0000-4000-8000-000000000099`))).code).toBe('not_found')
  })
})

describe('PATCH migration', () => {
  it('mengganti definisi draft, menaikkan revision, dan menghitung ulang checksum', async () => {
    const { client } = await signIn('editor')
    const before = await client.get<Schemas['Migration']>(`${base}/${w.migrationD}`)
    const replacement = { ...definition, title: 'Judul baru' }
    const updated = await client.patch<Schemas['Migration']>(`${base}/${w.migrationD}`, { ...replacement, revision: before.revision })
    expect(updated).toMatchObject({ title: 'Judul baru', revision: before.revision + 1, state: 'draft', up_sql: definition.up_sql })
    expect(updated.checksum).not.toBe(before.checksum)
    expect(updated.checksum).toBe(definitionChecksum(replacement as Parameters<typeof definitionChecksum>[0]))
  })

  it('revision usang adalah konflik dengan reason revision_mismatch', async () => {
    const { client } = await signIn('editor')
    await client.patch(`${base}/${w.migrationD}`, { ...definition, revision: 1 })
    const err = await failureOf(client.patch(`${base}/${w.migrationD}`, { ...definition, revision: 1 }))
    expect(err).toMatchObject({ status: 409, code: 'conflict', reason: 'revision_mismatch' })
  })

  it('migration yang sudah dibekukan atau dieksekusi tidak bisa diubah', async () => {
    const { client } = await signIn('editor')
    const err = await failureOf(client.patch(`${base}/${w.migrationA}`, { ...definition, revision: 1 }))
    expect(err).toMatchObject({ status: 409, reason: 'definition_frozen' })
    getState().migrations.find((migration) => migration.id === w.migrationD)!.state = 'frozen'
    expect((await failureOf(client.patch(`${base}/${w.migrationD}`, { ...definition, revision: 1 }))).reason).toBe('definition_frozen')
  })

  it('revision wajib, validasi tetap berlaku, dan view_only ditolak', async () => {
    const { client } = await signIn('editor')
    expect((await failureOf(client.patch(`${base}/${w.migrationD}`, definition))).fields.revision).toHaveLength(1)
    expect((await failureOf(client.patch(`${base}/${w.migrationD}`, { ...definition, up_sql: 'COMMIT;', revision: 1 }))).fields.up_sql).toHaveLength(1)
    const observer = await signIn('observer')
    expect((await failureOf(observer.client.patch(`${base}/${w.migrationD}`, { ...definition, revision: 1 }))).code).toBe('forbidden')
  })
})

describe('POST plan', () => {
  const plan = async (user: string, migrationId: string, body: unknown) => (await signIn(user)).client.post<Schemas['Plan']>(`${base}/${migrationId}/plan`, body)

  it('menjelaskan SELECT berindeks tanpa waktu eksekusi, dan dengan ANALYZE membawa waktu aktual', async () => {
    const { migration } = await create('editor', explainable)
    const plain = await plan('editor', migration.id, { revision: 1, direction: 'up', statement: 1, analyze: false })
    expect(plain).toMatchObject({ revision: 1, checksum: migration.checksum, direction: 'up', statement: 1, analyze: false, planning_ms: null, execution_ms: null })
    expect(plain.plan).toMatchObject({ node_type: 'Index Scan', relation: 'orders', index: 'orders_pkey' })
    expect(plain.plan.actual_rows).toBeUndefined()
    const analyzed = await plan('editor', migration.id, { revision: 1, direction: 'up', statement: 1, analyze: true })
    expect(analyzed.planning_ms).toBeGreaterThan(0)
    expect(analyzed.execution_ms).toBeGreaterThan(0)
    expect(analyzed.plan).toMatchObject({ actual_rows: 1, actual_loops: 1 })
  })

  it('UPDATE dijelaskan sebagai ModifyTable atas Seq Scan dengan jumlah baris tabel', async () => {
    const { migration } = await create('editor', explainable)
    const result = await plan('editor', migration.id, { revision: 1, direction: 'up', statement: 2, analyze: false })
    expect(result.plan).toMatchObject({ node_type: 'ModifyTable', relation: 'orders', plans: [{ node_type: 'Seq Scan', rows: 137 }] })
  })

  it('ANALYZE untuk statement yang mengubah data, DDL, dan nomor di luar jangkauan ditolak', async () => {
    const { migration } = await create('editor', explainable)
    const client = (await signIn('editor')).client
    const path = `${base}/${migration.id}/plan`
    expect((await failureOf(client.post(path, { revision: 1, direction: 'up', statement: 2, analyze: true }))).fields.analyze).toHaveLength(1)
    expect((await failureOf(client.post(path, { revision: 1, direction: 'up', statement: 3, analyze: false }))).fields.statement[0]).toContain('tidak dapat dijelaskan')
    expect((await failureOf(client.post(path, { revision: 1, direction: 'up', statement: 4, analyze: false }))).fields.statement[0]).toContain('di luar jangkauan')
  })

  it('mode batch, arah down pada migration irreversible, dan revision usang ditolak', async () => {
    const client = (await signIn('editor')).client
    expect((await failureOf(client.post(`${base}/${w.migrationC}/plan`, { revision: 1, direction: 'up', statement: 1, analyze: false }))).fields.statement).toHaveLength(1)
    expect((await failureOf(client.post(`${base}/${w.migrationE}/plan`, { revision: 1, direction: 'down', statement: 1, analyze: false }))).fields.direction).toHaveLength(1)
    expect((await failureOf(client.post(`${base}/${w.migrationD}/plan`, { revision: 9, direction: 'up', statement: 1, analyze: false }))).reason).toBe('revision_mismatch')
    expect((await failureOf(client.post(`${base}/${w.migrationD}/plan`, {}))).code).toBe('validation_failed')
  })

  it('hanya untuk user yang boleh mengedit', async () => {
    const err = await failureOf(plan('observer', w.migrationD, { revision: 1, direction: 'up', statement: 1, analyze: false }))
    expect(err.code).toBe('forbidden')
  })
})

describe('GET schema-diff', () => {
  const diff = async (user: string, query: Record<string, string>, database: string = ids.inventaris) => (await signIn(user)).client.get<Schemas['SchemaDiff']>(`/databases/${database}/schema-diff`, { query })

  it('membandingkan dua database: tabel yang hanya ada di tujuan ditambah dan sisanya dihapus sebagai destruktif', async () => {
    const result = await diff('reviewer', { desired_database_id: ids.pelaporan })
    expect(result).toMatchObject({ current_database_id: ids.inventaris, desired_database_id: ids.pelaporan, current_schema: 'public', desired_schema: 'public', destructive_count: 3 })
    expect(result.changes.filter((change) => change.action === 'add').map((change) => change.name)).toEqual(['laporan_harian', 'metrik'])
    expect(result.changes.filter((change) => change.destructive).map((change) => change.name).sort()).toEqual(['customers', 'orders', 'products'])
    expect(result.generated_up_sql).toContain('CREATE TABLE public.laporan_harian')
    expect(result.generated_up_sql).toContain('DROP TABLE public.orders;')
    expect(result.warnings).toHaveLength(3)
    expect(result.validation).toContain('lolos')
  })

  it('perbedaan kolom: tambah, ubah tipe, ubah nullable, dan hapus kolom', async () => {
    const products = getState().tables.find((table) => table.name === 'products')!
    getState().tables.push({
      ...products,
      database_id: ids.pelaporan,
      columns: [
        ...products.columns.filter((column) => column.name !== 'aktif').map((column) => (column.name === 'stok' ? { ...column, data_type: 'bigint' } : column.name === 'sku' ? { ...column, nullable: true } : column)),
        { name: 'warna', data_type: 'text', nullable: true, default: null },
      ],
    })
    const result = await diff('reviewer', { desired_database_id: ids.pelaporan })
    const byName = Object.fromEntries(result.changes.filter((change) => change.name.startsWith('products.')).map((change) => [change.name, change]))
    expect(byName['products.warna']).toMatchObject({ action: 'add', object: 'column', destructive: false })
    expect(byName['products.stok']).toMatchObject({ action: 'alter', before: 'integer', after: 'bigint' })
    expect(byName['products.sku']).toMatchObject({ action: 'alter', before: 'NOT NULL', after: 'NULL' })
    expect(byName['products.aktif']).toMatchObject({ action: 'drop', destructive: true })
    expect(result.generated_up_sql).toContain('ALTER TABLE public.products ADD COLUMN warna text;')
    expect(result.generated_up_sql).toContain('ALTER TABLE public.products ALTER COLUMN sku DROP NOT NULL;')
  })

  it('database yang sama tidak menghasilkan perbedaan', async () => {
    const result = await diff('reviewer', { desired_database_id: ids.inventaris })
    expect(result).toMatchObject({ changes: [], warnings: [], generated_up_sql: '', validation: 'Tidak ada perbedaan', destructive_count: 0 })
  })

  it('membaca kedua database dan hanya PostgreSQL yang didukung', async () => {
    expect((await failureOf(diff('observer', { desired_database_id: ids.pelaporan }))).code).toBe('forbidden')
    expect((await failureOf(diff('observer', { desired_database_id: ids.arsip }))).status).toBe(422)
    expect((await failureOf(diff('observer', { desired_database_id: ids.inventaris }, ids.arsip))).status).toBe(422)
  })

  it('parameter wajib, UUID valid, schema harus ada, dan query asing ditolak', async () => {
    expect((await failureOf(diff('reviewer', {}))).code).toBe('bad_request')
    expect((await failureOf(diff('reviewer', { desired_database_id: 'bukan-uuid' }))).code).toBe('bad_request')
    expect((await failureOf(diff('reviewer', { desired_database_id: ids.pelaporan, x: '1' }))).code).toBe('bad_request')
    const err = await failureOf(diff('reviewer', { desired_database_id: ids.pelaporan, current_schema: 'tidak_ada', desired_schema: 'juga_tidak' }))
    expect(err.fields.current_schema).toHaveLength(1)
    expect(err.fields.desired_schema).toHaveLength(1)
  })
})
