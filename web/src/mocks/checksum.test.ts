import { describe, expect, it } from 'vitest'
import fixtureText from '../../../api/examples/checksum-v1.json?raw'
import { definitionChecksum, sha256Hex } from './checksum.ts'

const fixture = JSON.parse(fixtureText) as { canonical_json: string; sha256: string }

describe('sha256Hex', () => {
  it.each([
    ['', 'e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855'],
    ['abc', 'ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad'],
    ['abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq', '248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1'],
  ])('menghitung %j sesuai vektor standar', (input, expected) => {
    expect(sha256Hex(input)).toBe(expected)
  })

  it('menangani input multi-blok dan karakter Unicode', () => {
    expect(sha256Hex('a'.repeat(1000))).toBe('41edece42d63e8d9bf515a9ba6932e1c20cbc9f5a5d134645adb5db1b9737ea3')
    expect(sha256Hex('é😀')).toHaveLength(64)
  })
})

describe('definitionChecksum', () => {
  it('cocok dengan vektor uji dari kontrak', () => {
    expect(sha256Hex(fixture.canonical_json)).toBe(fixture.sha256)
    const definition = JSON.parse(fixture.canonical_json) as Parameters<typeof definitionChecksum>[0]
    expect(definitionChecksum(definition)).toBe(fixture.sha256)
  })

  it('berubah jika judul, SQL, alasan, mode, atau batch berubah', () => {
    const base = { title: 't', up_sql: 'SELECT 1;', down_sql: 'SELECT 2;', irreversible: false, irreversible_reason: '', mode: 'normal' as const, batch: null }
    const checksum = definitionChecksum(base)
    expect(definitionChecksum({ ...base, title: 'u' })).not.toBe(checksum)
    expect(definitionChecksum({ ...base, up_sql: 'SELECT 3;' })).not.toBe(checksum)
    expect(definitionChecksum({ ...base, down_sql: null, irreversible: true, irreversible_reason: 'data hilang' })).not.toBe(checksum)
    const batch = { schema: 'public', table: 'a', key_column: 'id', batch_size: 100, scope_where: null }
    expect(definitionChecksum({ ...base, mode: 'batch', batch })).not.toBe(checksum)
  })

  it('predikat batch kosong disamakan dengan null', () => {
    const base = { title: 't', up_sql: 'x = 1', down_sql: 'x = 2', irreversible: false, irreversible_reason: '', mode: 'batch' as const }
    const batch = { schema: 'public', table: 'a', key_column: 'id', batch_size: 100 }
    expect(definitionChecksum({ ...base, batch: { ...batch, scope_where: '' } })).toBe(definitionChecksum({ ...base, batch: { ...batch, scope_where: null } }))
  })
})
