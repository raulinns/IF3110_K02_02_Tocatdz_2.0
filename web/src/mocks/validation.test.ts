import { describe, expect, it } from 'vitest'
import { ApiFailure } from './http.ts'
import { dsnError, Fields, nameError, normalizeUsername, passwordError, usernameError } from './validation.ts'

describe('username', () => {
  it('dinormalkan dengan trim dan huruf kecil', () => {
    expect(normalizeUsername('  Budi_01 ')).toBe('budi_01')
  })

  it.each(['ab', 'a'.repeat(33), 'ada spasi', 'huruf-strip', 'Besar'])('menolak %j', (username) => {
    expect(usernameError(username)).not.toBeNull()
  })

  it.each(['abc', 'budi_01', 'a'.repeat(32)])('menerima %j', (username) => {
    expect(usernameError(username)).toBeNull()
  })
})

describe('password', () => {
  it('menghitung karakter Unicode, bukan byte', () => {
    expect(passwordError('😀'.repeat(15))).toBeNull()
    expect(passwordError('😀'.repeat(14))).not.toBeNull()
  })

  it('membatasi 1024 byte', () => {
    expect(passwordError('a'.repeat(1024))).toBeNull()
    expect(passwordError('a'.repeat(1025))).not.toBeNull()
  })
})

describe('nameError', () => {
  it('menghitung rune dan menolak nama kosong', () => {
    expect(nameError('', 'Nama group')).toContain('Nama group')
    expect(nameError('😀'.repeat(64), 'Nama')).toBeNull()
    expect(nameError('g'.repeat(65), 'Nama')).not.toBeNull()
  })
})

describe('dsnError', () => {
  it('memeriksa bentuk DSN PostgreSQL dan batas ukuran', () => {
    expect(dsnError('', 'postgresql')).not.toBeNull()
    expect(dsnError('host=db', 'postgresql')).not.toBeNull()
    expect(dsnError('postgres://u:p@h/db', 'postgresql')).toBeNull()
    expect(dsnError('postgresql://u:p@h/db', 'postgresql')).toBeNull()
    expect(dsnError('u:p@tcp(h)/db', 'mysql')).toBeNull()
    expect(dsnError(`postgres://${'a'.repeat(4096)}`, 'postgresql')).not.toBeNull()
  })
})

describe('Fields', () => {
  it('tidak melempar jika tidak ada pesan', () => {
    const fields = new Fields()
    fields.add('a', null)
    expect(() => fields.check()).not.toThrow()
  })

  it('mengumpulkan beberapa pesan per field dalam satu error 422', () => {
    const fields = new Fields()
    fields.add('a', 'satu')
    fields.add('a', 'dua')
    fields.add('b', 'tiga')
    try {
      fields.check()
      throw new Error('seharusnya gagal')
    } catch (err) {
      expect(err).toBeInstanceOf(ApiFailure)
      const failure = err as ApiFailure
      expect(failure.status).toBe(422)
      expect(failure.extra.fields).toEqual({ a: ['satu', 'dua'], b: ['tiga'] })
    }
  })
})
