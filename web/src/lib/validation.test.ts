import { describe, expect, it } from 'vitest'
import { pageSearch, positiveInteger, readPage } from './pagination.ts'
import { dsnProblem, nameProblem, normalizeUsername, passwordProblem, usernameProblem } from './validation.ts'

describe('username', () => {
  it('dinormalkan dengan trim dan huruf kecil', () => {
    expect(normalizeUsername('  Budi_01 ')).toBe('budi_01')
  })

  it.each(['abc', 'budi_01', 'a'.repeat(32)])('menerima %j', (username) => {
    expect(usernameProblem(username)).toBeNull()
  })

  it.each(['ab', 'a'.repeat(33), 'ada spasi', 'huruf-strip', 'Besar'])('menolak %j', (username) => {
    expect(usernameProblem(username)).toContain('Username harus')
  })

  it('username kosong diminta diisi', () => {
    expect(usernameProblem('')).toBe('Username wajib diisi')
  })
})

describe('password', () => {
  it('menghitung karakter Unicode, bukan byte', () => {
    expect(passwordProblem('😀'.repeat(15))).toBeNull()
    expect(passwordProblem('😀'.repeat(14))).toBe('Password minimal 15 karakter')
  })

  it('wajib diisi dan dibatasi 1024 byte', () => {
    expect(passwordProblem('')).toBe('Password wajib diisi')
    expect(passwordProblem('a'.repeat(1024))).toBeNull()
    expect(passwordProblem('a'.repeat(1025))).toBe('Password maksimal 1024 byte')
  })

  it('spasi tidak dibuang dan tetap dihitung', () => {
    expect(passwordProblem(' '.repeat(15))).toBeNull()
  })
})

describe('nama dan connection string', () => {
  it('nama wajib dan maksimal 64 karakter', () => {
    expect(nameProblem('', 'Nama group')).toBe('Nama group wajib diisi')
    expect(nameProblem('😀'.repeat(64), 'Nama')).toBeNull()
    expect(nameProblem('g'.repeat(65), 'Nama')).toBe('Nama maksimal 64 karakter')
  })

  it('connection string wajib dan maksimal 4096 byte', () => {
    expect(dsnProblem('')).toBe('Connection string wajib diisi')
    expect(dsnProblem('a'.repeat(4096))).toBeNull()
    expect(dsnProblem('a'.repeat(4097))).toBe('Connection string terlalu panjang')
  })
})

describe('pagination', () => {
  it('membaca bilangan bulat positif dan mengabaikan yang lain', () => {
    expect(positiveInteger('3')).toBe(3)
    expect(positiveInteger('0')).toBeUndefined()
    expect(positiveInteger('01')).toBeUndefined()
    expect(positiveInteger('1.5')).toBeUndefined()
    expect(positiveInteger(null)).toBeUndefined()
    expect(positiveInteger('12345678')).toBeUndefined()
  })

  it('halaman dibaca dari query dan bawaannya 1', () => {
    expect(readPage(new URLSearchParams('page=4'))).toBe(4)
    expect(readPage(new URLSearchParams('page=abc'))).toBe(1)
    expect(readPage(new URLSearchParams(''))).toBe(1)
  })

  it('halaman pertama tidak ditulis ke URL', () => {
    expect(pageSearch(1)).toBe('')
    expect(pageSearch(2)).toBe('?page=2')
  })
})
