import { describe, expect, it } from 'vitest'
import { accessLabel, engineLabel, tableKindLabel } from './labels.ts'
import { databasePath, rowsPath, schemaPath, tablePath } from './paths.ts'

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
