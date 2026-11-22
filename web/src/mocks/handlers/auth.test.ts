import { describe, expect, it } from 'vitest'
import type { Schemas } from '../../api/types.ts'
import { demoPassword, ids } from '../seed.ts'
import { getState, loginFailureLimit } from '../state.ts'
import { failureOf, newClient, setupMockServer, signIn } from '../testing.ts'
import { authHandlers } from './auth.ts'

setupMockServer(...authHandlers)

const login = (username: string, password: string) => newClient().post('/auth/login', { username, password }, { handleUnauthenticated: false })

describe('POST /auth/login', () => {
  it('mengembalikan identitas, token CSRF, dan group yang dipimpin tanpa rahasia', async () => {
    const { me } = await signIn('reviewer')
    expect(me.user).toEqual({ id: ids.reviewer, username: 'reviewer', active: true, platform_admin: false })
    expect(me.platform_admin).toBe(false)
    expect(me.csrf_token).toBeTruthy()
    expect(me.leader_of).toEqual([ids.writers])
    expect(JSON.stringify(me)).not.toContain(demoPassword)
  })

  it('menormalkan username dengan trim dan huruf kecil', async () => {
    const { me } = await signIn('  Reviewer ')
    expect(me.user.username).toBe('reviewer')
  })

  it('admin bukan leader mana pun', async () => {
    const { me } = await signIn('admin')
    expect(me.platform_admin).toBe(true)
    expect(me.leader_of).toEqual([])
  })

  it('kegagalan selalu generik, baik password salah, user tidak ada, maupun user nonaktif', async () => {
    const errors = await Promise.all([failureOf(login('reviewer', 'salah')), failureOf(login('tidak-ada', demoPassword)), failureOf(login('nonaktif', demoPassword))])
    for (const err of errors) {
      expect(err.status).toBe(401)
      expect(err.code).toBe('unauthenticated')
      expect(err.message).toBe(errors[0].message)
    }
  })

  it('field yang kosong ditandai per field sebagai daftar pesan', async () => {
    const err = await failureOf(newClient().post('/auth/login', {}, { handleUnauthenticated: false }))
    expect(err.code).toBe('validation_failed')
    expect(err.fields.username).toHaveLength(1)
    expect(err.fields.password).toHaveLength(1)
  })

  it('menolak field asing dan tipe yang salah dengan 400', async () => {
    expect((await failureOf(newClient().post('/auth/login', { username: 'a', password: 'b', extra: 1 }, { handleUnauthenticated: false }))).code).toBe('bad_request')
    expect((await failureOf(newClient().post('/auth/login', { username: 1, password: 'b' }, { handleUnauthenticated: false }))).code).toBe('bad_request')
  })

  it('menolak Content-Type selain JSON dengan 415', async () => {
    const res = await fetch('http://localhost/api/v1/auth/login', { method: 'POST', headers: { 'Content-Type': 'text/plain' }, body: '{}' })
    expect(res.status).toBe(415)
    expect(res.headers.get('X-Request-ID')).toBeTruthy()
    expect(res.headers.get('Cache-Control')).toBe('no-store')
  })

  it('dikunci setelah beberapa kegagalan dan Retry-After menghitung mundur', async () => {
    for (let i = 0; i < loginFailureLimit; i++) await failureOf(login('reviewer', 'salah'))
    const locked = await failureOf(login('reviewer', demoPassword))
    expect(locked.status).toBe(429)
    expect(locked.code).toBe('rate_limited')
    expect(locked.retryAfter).toBeGreaterThan(0)
    expect(locked.retryAfter).toBeLessThanOrEqual(60)
    expect(locked.requestId).toBeTruthy()
    getState().lockedUntil = 0
    await expect(login('reviewer', demoPassword)).resolves.toBeTruthy()
  })

  it('login berhasil mengulang hitungan kegagalan', async () => {
    for (let round = 0; round < 3; round++) {
      for (let i = 0; i < loginFailureLimit - 1; i++) await failureOf(login('reviewer', 'salah'))
      await expect(login('reviewer', demoPassword)).resolves.toBeTruthy()
    }
  })
})

describe('GET /auth/me', () => {
  it('menolak request tanpa sesi dengan 401', async () => {
    const err = await failureOf(newClient().get('/auth/me', { handleUnauthenticated: false }))
    expect(err.status).toBe(401)
    expect(err.code).toBe('unauthenticated')
  })

  it('mengembalikan identitas dan token yang sama dengan hasil login', async () => {
    const { client, me } = await signIn('observer')
    await expect(client.get('/auth/me')).resolves.toEqual(me)
  })

  it('menolak nama query yang tidak dikenal', async () => {
    const { client } = await signIn('observer')
    expect((await failureOf(client.get('/auth/me', { query: { x: 1 } }))).code).toBe('bad_request')
  })

  it('user yang dinonaktifkan saat sesi berjalan langsung ditolak', async () => {
    const { client } = await signIn('editor')
    getState().users.find((user) => user.id === ids.editor)!.active = false
    expect((await failureOf(client.get('/auth/me', { handleUnauthenticated: false }))).status).toBe(401)
  })
})

describe('POST /auth/logout', () => {
  it('mengakhiri sesi', async () => {
    const { client } = await signIn('reviewer')
    await expect(client.post('/auth/logout')).resolves.toBeUndefined()
    expect((await failureOf(client.get('/auth/me', { handleUnauthenticated: false }))).status).toBe(401)
  })

  it('tanpa sesi menghasilkan 401, bukan kegagalan CSRF', async () => {
    const err = await failureOf(newClient().post('/auth/logout', undefined, { handleUnauthenticated: false }))
    expect(err.code).toBe('unauthenticated')
  })

  it('menolak token CSRF yang kosong atau salah tanpa mengakhiri sesi', async () => {
    const { client } = await signIn('reviewer')
    expect((await failureOf(newClient().post('/auth/logout'))).code).toBe('csrf_failed')
    client.configure({ csrfToken: 'salah' })
    expect((await failureOf(client.post('/auth/logout'))).code).toBe('csrf_failed')
    await expect(client.get('/auth/me')).resolves.toBeTruthy()
  })

  it('client mengambil token baru lalu mengulang jika token basi', async () => {
    const { client } = await signIn('reviewer')
    client.configure({ csrfToken: 'basi', refreshCsrf: async () => (await client.get<Schemas['AuthMe']>('/auth/me')).csrf_token })
    await expect(client.post('/auth/logout')).resolves.toBeUndefined()
  })

  it('menolak request yang membawa isi walau hanya object kosong', async () => {
    const { me } = await signIn('reviewer')
    const res = await fetch('http://localhost/api/v1/auth/logout', { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': me.csrf_token }, body: '{}' })
    expect(res.status).toBe(400)
  })
})
