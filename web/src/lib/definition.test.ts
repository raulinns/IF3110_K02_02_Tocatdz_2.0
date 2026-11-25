import { describe, expect, it } from 'vitest'
import { definitionProblems, emptyForm, firstProblem, formFromMigration, sameForm, toDefinition, type DefinitionForm } from './definition.ts'

const valid = (): DefinitionForm => ({ ...emptyForm(), title: 'Tambah kolom', upSql: 'ALTER TABLE t ADD COLUMN a int;', downSql: 'ALTER TABLE t DROP COLUMN a;' })
const batch = (): DefinitionForm => ({ ...valid(), mode: 'batch', upSql: "status = 'arsip'", downSql: "status = 'aktif'", batch: { schema: 'public', table: 'orders', keyColumn: 'id', batchSize: '1000', scopeWhere: '' } })

describe('toDefinition', () => {
  it('mode normal tidak membawa konfigurasi batch dan SQL dikirim apa adanya', () => {
    const form = { ...valid(), upSql: '  SELECT 1;  \n', title: ' Judul ' }
    expect(toDefinition(form)).toEqual({ title: ' Judul ', up_sql: '  SELECT 1;  \n', down_sql: 'ALTER TABLE t DROP COLUMN a;', irreversible: false, irreversible_reason: '', mode: 'normal', batch: null })
  })

  it('irreversible mengirim down_sql null dan alasan, serta membuang alasan saat tidak irreversible', () => {
    expect(toDefinition({ ...valid(), irreversible: true, reason: 'Data dihapus' })).toMatchObject({ down_sql: null, irreversible: true, irreversible_reason: 'Data dihapus' })
    expect(toDefinition({ ...valid(), reason: 'sisa ketikan' })).toMatchObject({ irreversible: false, irreversible_reason: '' })
  })

  it('mode batch mengirim ukuran sebagai angka dan predikat kosong sebagai null', () => {
    expect(toDefinition(batch()).batch).toEqual({ schema: 'public', table: 'orders', key_column: 'id', batch_size: 1000, scope_where: null })
    const scoped = { ...batch(), batch: { ...batch().batch, scopeWhere: "status = 'a'" } }
    expect(toDefinition(scoped).batch?.scope_where).toBe("status = 'a'")
    const blank = { ...batch(), batch: { ...batch().batch, scopeWhere: '   ' } }
    expect(toDefinition(blank).batch?.scope_where).toBeNull()
  })
})

describe('formFromMigration', () => {
  it('membalik toDefinition untuk migration normal, irreversible, dan batch', () => {
    for (const form of [valid(), { ...valid(), downSql: '', irreversible: true, reason: 'alasan' }, batch()]) {
      expect(formFromMigration(toDefinition(form))).toEqual(form)
    }
  })

  it('migration irreversible menghasilkan down kosong dan batch tersembunyi memakai nilai bawaan', () => {
    const form = formFromMigration({ ...toDefinition(valid()), down_sql: null, irreversible: true, irreversible_reason: 'x' })
    expect(form.downSql).toBe('')
    expect(form.batch.schema).toBe('public')
  })
})

describe('definitionProblems', () => {
  it('definisi lengkap tidak bermasalah', () => {
    expect(definitionProblems(valid())).toEqual({})
    expect(definitionProblems(batch())).toEqual({})
  })

  it('judul, SQL up, dan SQL down wajib diisi', () => {
    const problems = definitionProblems({ ...emptyForm() })
    expect(problems.title).toEqual(['Judul wajib diisi'])
    expect(problems.up_sql).toEqual(['SQL up wajib diisi'])
    expect(problems.down_sql).toEqual(['SQL down wajib diisi atau tandai migration irreversible'])
  })

  it('judul dihitung dalam karakter Unicode dan dibatasi 200', () => {
    expect(definitionProblems({ ...valid(), title: '😀'.repeat(200) }).title).toBeUndefined()
    expect(definitionProblems({ ...valid(), title: 'a'.repeat(201) }).title).toEqual(['Judul maksimal 200 karakter'])
  })

  it('SQL hanya spasi dianggap kosong dan SQL terlalu besar ditolak', () => {
    expect(definitionProblems({ ...valid(), upSql: '  \n ' }).up_sql).toEqual(['SQL up wajib diisi'])
    expect(definitionProblems({ ...valid(), upSql: 'a'.repeat(524289) }).up_sql).toEqual(['SQL up maksimal 524288 byte'])
  })

  it('irreversible meminta alasan dan tidak lagi meminta SQL down', () => {
    const problems = definitionProblems({ ...valid(), downSql: '', irreversible: true, reason: '  ' })
    expect(problems.irreversible_reason).toEqual(['Alasan irreversible wajib diisi'])
    expect(problems.down_sql).toBeUndefined()
    expect(definitionProblems({ ...valid(), downSql: '', irreversible: true, reason: 'ada' })).toEqual({})
  })

  it('mode batch memeriksa identifier dan ukuran batch', () => {
    const bad = { ...batch(), batch: { schema: '', table: 'bad name', keyColumn: '1id', batchSize: '99', scopeWhere: '' } }
    expect(Object.keys(definitionProblems(bad)).sort()).toEqual(['batch.batch_size', 'batch.key_column', 'batch.schema', 'batch.table'])
    for (const size of ['100', '100000']) expect(definitionProblems({ ...batch(), batch: { ...batch().batch, batchSize: size } })['batch.batch_size']).toBeUndefined()
    for (const size of ['', '99', '100001', '1.5', 'abc']) expect(definitionProblems({ ...batch(), batch: { ...batch().batch, batchSize: size } })['batch.batch_size']).toBeDefined()
  })

  it('label SQL menyesuaikan mode batch', () => {
    expect(definitionProblems({ ...batch(), upSql: '' }).up_sql).toEqual(['Bagian SET wajib diisi'])
  })

  it('isian batch yang tersembunyi di mode normal tidak diperiksa', () => {
    expect(definitionProblems({ ...valid(), batch: { schema: '', table: '', keyColumn: '', batchSize: '', scopeWhere: '' } })).toEqual({})
  })
})

describe('firstProblem dan sameForm', () => {
  it('firstProblem mengikuti urutan form', () => {
    expect(firstProblem({ up_sql: ['x'], title: ['y'] })).toBe('title')
    expect(firstProblem({ 'batch.table': ['x'], down_sql: ['y'] })).toBe('down_sql')
    expect(firstProblem({})).toBeUndefined()
  })

  it('sameForm membandingkan isi, bukan referensi', () => {
    expect(sameForm(valid(), valid())).toBe(true)
    expect(sameForm(valid(), { ...valid(), downSql: 'x' })).toBe(false)
  })
})
