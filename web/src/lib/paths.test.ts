import { describe, expect, it } from 'vitest'
import { accessLabel, engineLabel, tableKindLabel } from './labels.ts'
import { databasePath, historyPath, jobPath, jobsPath, migrationPath, migrationsPath, newMigrationPath, requestPath, reviewPath, rowsPath, schemaDiffPath, schemaPath, tablePath } from './paths.ts'

describe('paths', () => {
  it('menyusun path explorer', () => {
    expect(databasePath('abc')).toBe('/databases/abc')
    expect(schemaPath('abc')).toBe('/databases/abc/schema')
    expect(tablePath('abc', 'public', 'orders')).toBe('/databases/abc/schema/public/orders')
    expect(rowsPath('abc', 'public', 'orders')).toBe('/databases/abc/schema/public/orders/data')
  })

  it('meng-encode setiap segmen supaya spasi, garis miring, dan tanda tanya tidak mengubah route', () => {
    expect(tablePath('abc', 'sch ema', 'tabel/unik?1')).toBe('/databases/abc/schema/sch%20ema/tabel%2Funik%3F1')
    expect(rowsPath('abc', '100%', '#x')).toBe('/databases/abc/schema/100%25/%23x/data')
  })
})

describe('labels', () => {
  it('label akses mengikuti halaman M1', () => {
    expect(accessLabel('view_edit')).toBe('Lihat + ubah')
    expect(accessLabel('view_only')).toBe('Lihat saja')
  })

  it('label engine dan jenis tabel', () => {
    expect(engineLabel('postgresql')).toBe('PostgreSQL')
    expect(engineLabel('mysql')).toBe('MySQL')
    expect(tableKindLabel('partitioned_table')).toBe('partitioned table')
    expect(tableKindLabel('materialized_view')).toBe('materialized view')
    expect(tableKindLabel('view')).toBe('view')
  })
})

describe('path migration', () => {
  it('menyusun path migration dengan segmen yang di-encode', () => {
    expect(migrationsPath('db 1')).toBe('/databases/db%201/migrations')
    expect(migrationPath('db', 'a/b')).toBe('/databases/db/migrations/a%2Fb')
    expect(newMigrationPath('db')).toBe('/databases/db/migrations/new')
    expect(schemaDiffPath('db')).toBe('/databases/db/schema-diff')
    expect(reviewPath('db')).toBe('/databases/db/review')
  })
})

describe('path workflow', () => {
  it('menyusun path review, job, dan riwayat dengan segmen yang di-encode', () => {
    expect(requestPath('db', 'a b')).toBe('/databases/db/review/a%20b')
    expect(jobsPath('db')).toBe('/databases/db/jobs')
    expect(jobPath('db', 'j/1')).toBe('/databases/db/jobs/j%2F1')
    expect(historyPath('db')).toBe('/databases/db/history')
  })
})
