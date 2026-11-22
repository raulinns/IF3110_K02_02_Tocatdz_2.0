import { describe, expect, it } from 'vitest'
import { firstKeyword, redactLiterals, splitStatements, sqlProblem } from './sql.ts'

describe('splitStatements', () => {
  it('memecah menurut titik koma dan membuang komentar serta statement kosong', () => {
    const sql = "-- judul\nCREATE TABLE a (id int); /* catatan */ INSERT INTO a VALUES (1);;\n  "
    expect(splitStatements(sql)).toEqual(['CREATE TABLE a (id int)', 'INSERT INTO a VALUES (1)'])
  })

  it('titik koma di dalam string atau identifier tidak memecah statement', () => {
    expect(splitStatements(`SELECT 'a;b' AS "x;y"; SELECT 2`)).toEqual([`SELECT 'a;b' AS "x;y"`, 'SELECT 2'])
  })

  it('kutip yang digandakan di dalam string tidak mengakhiri string', () => {
    expect(splitStatements("SELECT 'it''s'; SELECT 2")).toEqual(["SELECT 'it''s'", 'SELECT 2'])
  })
})

describe('sqlProblem mode normal', () => {
  it.each([
    'CREATE TABLE public.a (id bigint PRIMARY KEY);',
    "UPDATE public.orders SET status = 'arsip' WHERE status = 'selesai';",
    'ALTER TABLE a ADD COLUMN b int;\nCREATE INDEX idx_a ON a (b);',
    '-- mock:fail\nSELECT 1;',
  ])('menerima %j', (sql) => {
    expect(sqlProblem(sql, 'normal')).toBeNull()
  })

  it.each([
    ['SELECT (1;', 'sintaks'],
    ["SELECT 'belum ditutup", 'sintaks'],
    ['/* belum ditutup SELECT 1', 'sintaks'],
    ['BEGIN; SELECT 1;', 'transaksi'],
    ['COMMIT;', 'transaksi'],
    ['CREATE INDEX CONCURRENTLY i ON a (b);', 'transaksi migration'],
    ['VACUUM a;', 'transaksi migration'],
    ['SELECT * FROM information_schema.tables;', 'internal'],
    ['SELECT * FROM _tocatdz_migration_receipts;', 'internal'],
    ['DROP DATABASE x;', 'belum diizinkan'],
    ['GRANT ALL ON a TO b;', 'belum diizinkan'],
    ['CREATE EXTENSION pgcrypto;', 'belum diizinkan'],
  ])('menolak %j', (sql, fragment) => {
    expect(sqlProblem(sql, 'normal')).toContain(fragment)
  })

  it('kata terlarang di dalam string atau komentar tidak dianggap', () => {
    expect(sqlProblem("INSERT INTO log VALUES ('commit information_schema');", 'normal')).toBeNull()
    expect(sqlProblem('-- BEGIN\nSELECT 1;', 'normal')).toBeNull()
  })
})

describe('sqlProblem mode batch', () => {
  it('menerima satu bagian assignment', () => {
    expect(sqlProblem("status = 'arsip'", 'batch')).toBeNull()
    expect(sqlProblem('harga = harga * 1.05, stok = 0', 'batch')).toBeNull()
  })

  it.each([
    ["UPDATE orders SET status = 'x'", 'tanpa kata'],
    ["SET status = 'x'", 'tanpa kata'],
    ["status = 'a'; status = 'b'", 'satu bagian'],
    ['bukan assignment', 'kolom = ekspresi'],
    ['(status = 1', 'sintaks'],
  ])('menolak %j', (sql, fragment) => {
    expect(sqlProblem(sql, 'batch')).toContain(fragment)
  })
})

describe('redactLiterals dan firstKeyword', () => {
  it('menyamarkan literal string termasuk yang memuat kutip ganda', () => {
    expect(redactLiterals("INSERT INTO a VALUES ('rahasia', 'it''s', 5)")).toBe("INSERT INTO a VALUES ('[REDACTED]', '[REDACTED]', 5)")
  })

  it('mengambil kata pertama dengan huruf kecil', () => {
    expect(firstKeyword('  UPDATE a SET b = 1')).toBe('update')
    expect(firstKeyword('')).toBe('')
  })
})
