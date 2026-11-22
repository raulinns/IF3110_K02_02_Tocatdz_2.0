import { describe, expect, it } from 'vitest'
import type { Definition } from './checksum.ts'
import { normalizeDefinition, readDefinition, validateDefinition } from './definition.ts'
import { ApiFailure } from './http.ts'
import { Fields } from './validation.ts'

const valid: Definition = {
  title: 'Buat tabel',
  up_sql: 'CREATE TABLE public.a (id bigint PRIMARY KEY);',
  down_sql: 'DROP TABLE public.a;',
  irreversible: false,
  irreversible_reason: '',
  mode: 'normal',
  batch: null,
}
const batch = { schema: 'public', table: 'orders', key_column: 'id', batch_size: 100, scope_where: null }

function problems(definition: Definition): Record<string, string[]> {
  const fields = new Fields()
  validateDefinition(definition, fields)
  try {
    fields.check()
  } catch (err) {
    return (err as ApiFailure).extra.fields ?? {}
  }
  return {}
}

describe('readDefinition', () => {
  it('membaca body lengkap dan mengisi nilai default untuk field yang tidak dikirim', () => {
    expect(readDefinition({ ...valid })).toEqual(valid)
    expect(readDefinition({})).toEqual({ title: '', up_sql: '', down_sql: null, irreversible: false, irreversible_reason: '', mode: '', batch: null })
  })

  it('menolak tipe yang salah dan field batch asing dengan 400', () => {
    for (const body of [{ title: 1 }, { irreversible: 'ya' }, { down_sql: 5 }, { batch: 'x' }, { batch: [] }, { batch: { ...batch, extra: 1 } }, { batch: { ...batch, batch_size: '100' } }, { batch: { ...batch, scope_where: 5 } }]) {
      expect(() => readDefinition(body)).toThrow(ApiFailure)
    }
  })
})

describe('validateDefinition', () => {
  it('menerima definisi normal, irreversible, dan batch yang benar', () => {
    expect(problems(valid)).toEqual({})
    expect(problems({ ...valid, down_sql: null, irreversible: true, irreversible_reason: 'Data tidak bisa dipulihkan' })).toEqual({})
    expect(problems({ ...valid, mode: 'batch', up_sql: "status = 'arsip'", down_sql: "status = 'selesai'", batch })).toEqual({})
  })

  it('menandai judul dan SQL up yang kosong, berlebih, atau ditolak validator', () => {
    expect(problems({ ...valid, title: '   ' }).title).toHaveLength(1)
    expect(problems({ ...valid, title: 'x'.repeat(201) }).title).toHaveLength(1)
    expect(problems({ ...valid, up_sql: '  ' }).up_sql).toEqual(['SQL up wajib diisi'])
    expect(problems({ ...valid, up_sql: '-- hanya komentar' }).up_sql).toEqual(['SQL up wajib diisi'])
    expect(problems({ ...valid, up_sql: `SELECT '${'x'.repeat(524288)}';` }).up_sql[0]).toContain('maksimal')
    expect(problems({ ...valid, up_sql: 'BEGIN;' }).up_sql[0]).toContain('transaksi')
  })

  it('SQL down wajib kecuali migration irreversible dan divalidasi seperti SQL up', () => {
    expect(problems({ ...valid, down_sql: null }).down_sql[0]).toContain('irreversible')
    expect(problems({ ...valid, down_sql: 'COMMIT;' }).down_sql[0]).toContain('transaksi')
    expect(problems({ ...valid, down_sql: null, irreversible: true, irreversible_reason: '' }).irreversible_reason).toHaveLength(1)
    expect(problems({ ...valid, irreversible: true, irreversible_reason: 'alasan' }).down_sql).toHaveLength(1)
    expect(problems({ ...valid, irreversible_reason: 'tidak perlu' }).irreversible_reason).toHaveLength(1)
  })

  it('mode harus dikenal dan mode normal tidak boleh membawa batch', () => {
    expect(problems({ ...valid, mode: '' as Definition['mode'] }).mode).toHaveLength(1)
    expect(problems({ ...valid, batch }).batch).toHaveLength(1)
  })

  it('mode batch mewajibkan konfigurasi dan memeriksa setiap bagiannya per path', () => {
    const base = { ...valid, mode: 'batch' as const, up_sql: "status = 'arsip'", down_sql: "status = 'selesai'" }
    expect(problems({ ...base, batch: null }).batch).toHaveLength(1)
    const bad = problems({ ...base, batch: { schema: '', table: 'bad name', key_column: '1id', batch_size: 99, scope_where: 'a = 1; DROP TABLE x' } })
    expect(Object.keys(bad).sort()).toEqual(['batch.batch_size', 'batch.key_column', 'batch.schema', 'batch.scope_where', 'batch.table'])
    expect(problems({ ...base, batch: { ...batch, batch_size: 100001 } })['batch.batch_size']).toHaveLength(1)
    expect(problems({ ...base, batch: { ...batch, batch_size: 150.5 } })['batch.batch_size']).toHaveLength(1)
    expect(problems({ ...base, batch: { ...batch, scope_where: "status = 'selesai' AND id > 5" } })).toEqual({})
    expect(problems({ ...base, up_sql: "UPDATE orders SET status = 'x'", batch })['up_sql'][0]).toContain('tanpa kata')
  })

  it('SQL up yang salah dan down yang salah dilaporkan bersamaan per editor', () => {
    const result = problems({ ...valid, up_sql: 'BEGIN;', down_sql: 'COMMIT;' })
    expect(Object.keys(result).sort()).toEqual(['down_sql', 'up_sql'])
  })
})

describe('normalizeDefinition', () => {
  it('hanya merapikan judul dan tidak menyentuh byte SQL', () => {
    const result = normalizeDefinition({ ...valid, title: '  Judul ', up_sql: '  SELECT 1;  ' })
    expect(result.title).toBe('Judul')
    expect(result.up_sql).toBe('  SELECT 1;  ')
  })
})
