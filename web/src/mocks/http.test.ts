import { describe, expect, it } from 'vitest'
import { ApiFailure, booleanField, pageOf, readJson, readPaging, readQuery, requireNoBody, sortBy, stringField } from './http.ts'

async function failureOf(work: () => unknown): Promise<ApiFailure> {
  try {
    await work()
  } catch (err) {
    if (err instanceof ApiFailure) return err
    throw err
  }
  throw new Error('seharusnya gagal')
}

const url = (query: string) => new URL(`http://localhost/x${query}`)

function jsonRequest(body: string, type = 'application/json') {
  return new Request('http://localhost/x', { method: 'POST', headers: { 'Content-Type': type }, body })
}

describe('readPaging', () => {
  it('memakai default page 1 dan limit 50', () => {
    expect(readPaging(url(''))).toEqual({ page: 1, limit: 50 })
  })

  it('membaca page dan limit yang valid', () => {
    expect(readPaging(url('?page=3&limit=200'))).toEqual({ page: 3, limit: 200 })
  })

  it.each(['?page=0', '?page=01', '?page=-1', '?page=abc', '?limit=201', '?limit=1.5', '?page=1000000&limit=200'])('menolak %s', async (query) => {
    expect((await failureOf(() => readPaging(url(query)))).status).toBe(400)
  })
})

describe('readQuery', () => {
  it('menolak nama query yang tidak dikenal dan yang berulang', async () => {
    expect((await failureOf(() => readQuery(url('?x=1'), ['page']))).code).toBe('bad_request')
    expect((await failureOf(() => readQuery(url('?page=1&page=2'), ['page']))).code).toBe('bad_request')
  })

  it('menerima query kosong dan nama yang diizinkan', () => {
    expect(readQuery(url(''), []).size).toBe(0)
    expect(readQuery(url('?page=2'), ['page']).get('page')).toBe('2')
  })
})

describe('pageOf', () => {
  const items = [1, 2, 3, 4, 5]

  it('memotong halaman dan menandai halaman berikutnya', () => {
    expect(pageOf(items, { page: 1, limit: 2 })).toEqual({ items: [1, 2], page: 1, limit: 2, has_next: true })
    expect(pageOf(items, { page: 3, limit: 2 })).toEqual({ items: [5], page: 3, limit: 2, has_next: false })
  })

  it('halaman di luar jangkauan menghasilkan items kosong', () => {
    expect(pageOf(items, { page: 9, limit: 2 })).toEqual({ items: [], page: 9, limit: 2, has_next: false })
  })
})

describe('sortBy', () => {
  it('mengurutkan menurut kunci pertama lalu kunci berikutnya', () => {
    const rows = [
      { name: 'b', id: '1' },
      { name: 'a', id: '2' },
      { name: 'a', id: '1' },
    ]
    const sorted = [...rows].sort(sortBy<(typeof rows)[number]>((row) => row.name, (row) => row.id))
    expect(sorted.map((row) => `${row.name}${row.id}`)).toEqual(['a1', 'a2', 'b1'])
  })
})

describe('readJson', () => {
  it('membaca object JSON dan menerima charset utf-8', async () => {
    await expect(readJson(jsonRequest('{"a":1}'), ['a'])).resolves.toEqual({ a: 1 })
    await expect(readJson(jsonRequest('{"a":1}', 'application/json; charset=utf-8'), ['a'])).resolves.toEqual({ a: 1 })
  })

  it('menolak Content-Type selain JSON dengan 415', async () => {
    expect((await failureOf(() => readJson(jsonRequest('{}', 'text/plain'), []))).status).toBe(415)
  })

  it.each(['', 'bukan json', '[]', '"teks"', 'null', '{"a":1} {"b":2}'])('menolak isi %j dengan 400', async (body) => {
    expect((await failureOf(() => readJson(jsonRequest(body), ['a', 'b']))).status).toBe(400)
  })

  it('menolak field yang tidak dikenal', async () => {
    expect((await failureOf(() => readJson(jsonRequest('{"a":1,"x":2}'), ['a']))).code).toBe('bad_request')
  })
})

describe('requireNoBody', () => {
  it('menolak request yang membawa isi walau hanya object kosong', async () => {
    await expect(requireNoBody(new Request('http://localhost/x', { method: 'PUT' }))).resolves.toBeUndefined()
    expect((await failureOf(() => requireNoBody(jsonRequest('{}')))).status).toBe(400)
  })
})

describe('stringField dan booleanField', () => {
  it('mengembalikan undefined jika tidak dikirim dan menolak tipe yang salah', async () => {
    expect(stringField({}, 'a')).toBeUndefined()
    expect(booleanField({}, 'a')).toBeUndefined()
    expect(stringField({ a: 'x' }, 'a')).toBe('x')
    expect(booleanField({ a: false }, 'a')).toBe(false)
    expect((await failureOf(() => stringField({ a: 1 }, 'a'))).status).toBe(400)
    expect((await failureOf(() => booleanField({ a: 'ya' }, 'a'))).status).toBe(400)
  })
})
