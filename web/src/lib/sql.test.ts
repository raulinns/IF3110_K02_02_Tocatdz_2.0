import { describe, expect, it } from 'vitest'
import { splitStatements } from './sql.ts'

describe('splitStatements', () => {
  it('memecah menurut titik koma dan membuang komentar serta baris kosong', () => {
    expect(splitStatements('SELECT 1; -- komentar\nSELECT 2;;\n  ')).toEqual(['SELECT 1', 'SELECT 2'])
    // Komentar blok diganti satu spasi sehingga tiga spasi tersisa di antara kata
    expect(splitStatements('SELECT /* a; b */ 1')).toEqual(['SELECT   1'])
  })

  it('titik koma di dalam string tidak memecah statement', () => {
    expect(splitStatements("INSERT INTO t VALUES ('a;b'); SELECT \"x;y\"")).toEqual(["INSERT INTO t VALUES ('a;b')", 'SELECT "x;y"'])
  })

  it('kutip yang digandakan tetap berada dalam string', () => {
    expect(splitStatements("SELECT 'it''s; ok'; SELECT 2")).toEqual(["SELECT 'it''s; ok'", 'SELECT 2'])
  })

  it('SQL kosong atau hanya komentar tidak menghasilkan statement', () => {
    expect(splitStatements('')).toEqual([])
    expect(splitStatements('-- hanya komentar')).toEqual([])
  })
})
