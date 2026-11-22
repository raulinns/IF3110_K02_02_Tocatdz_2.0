import { describe, expect, it } from 'vitest'
import type { Schemas } from '../../api/types.ts'
import { demoPassword, ids } from '../seed.ts'
import { getState } from '../state.ts'
import { failureOf, newClient, setupMockServer, signIn } from '../testing.ts'
import { adminHandlers } from './admin.ts'
import { authHandlers } from './auth.ts'

setupMockServer(...authHandlers, ...adminHandlers)

type Page<T> = { items: T[]; page: number; limit: number; has_next: boolean }

const asAdmin = async () => (await signIn('admin')).client
const adminState = () => JSON.stringify([getState().users, getState().groups, getState().connections, getState().grants])
const strongPassword = 'kata-sandi-baru-123'

const endpoints: [method: 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE', path: string, body?: unknown][] = [
  ['GET', '/admin/users'],
  ['POST', '/admin/users', { username: 'baru_x', password: strongPassword }],
  ['PATCH', `/admin/users/${ids.editor}`, { active: false }],
  ['GET', '/admin/groups'],
  ['POST', '/admin/groups', { name: 'baru' }],
  ['PATCH', `/admin/groups/${ids.writers}`, { name: 'baru' }],
  ['DELETE', `/admin/groups/${ids.writers}`],
  ['GET', '/admin/connections'],
  ['POST', '/admin/connections', { name: 'baru', engine: 'postgresql', dsn: 'postgres://u:p@h/db' }],
  ['DELETE', `/admin/connections/${ids.arsip}`],
  ['GET', `/admin/groups/${ids.writers}/members`],
  ['PUT', `/admin/groups/${ids.writers}/members/${ids.observer}`],
  ['DELETE', `/admin/groups/${ids.writers}/members/${ids.editor}`],
  ['PUT', `/admin/groups/${ids.writers}/leader`, { user_id: ids.editor }],
  ['GET', '/admin/grants'],
  ['PUT', `/admin/grants/${ids.magang}/${ids.arsip}`, { access: 'view_only' }],
  ['DELETE', `/admin/grants/${ids.magang}/${ids.pelaporan}`],
]

describe('hak akses admin', () => {
  it.each(endpoints)('%s %s ditolak untuk non-admin tanpa mengubah data', async (method, path, body) => {
    const { client } = await signIn('reviewer')
    const before = adminState()
    const err = await failureOf(client.request(method, path, body))
    expect(err.status).toBe(403)
    expect(err.code).toBe('forbidden')
    expect(adminState()).toBe(before)
  })

  it('menolak request tanpa sesi dengan 401', async () => {
    expect((await failureOf(newClient().get('/admin/users', { handleUnauthenticated: false }))).status).toBe(401)
  })

  it('memeriksa token CSRF sebelum hak akses', async () => {
    const { client } = await signIn('reviewer')
    client.configure({ csrfToken: 'salah' })
    expect((await failureOf(client.post('/admin/groups', { name: 'x' }))).code).toBe('csrf_failed')
  })
})

describe('permintaan yang ketat', () => {
  it('menolak query asing, query berulang, dan limit berlebih', async () => {
    const client = await asAdmin()
    expect((await failureOf(client.get('/admin/users', { query: { foo: 1 } }))).code).toBe('bad_request')
    expect((await failureOf(client.get('/admin/users', { query: { limit: 201 } }))).code).toBe('bad_request')
    expect((await fetch('http://localhost/api/v1/admin/users?page=1&page=2')).status).toBe(400)
  })

  it('menolak ID path yang bukan UUID, dan ID yang tidak ada menjadi 404', async () => {
    const client = await asAdmin()
    expect((await failureOf(client.patch('/admin/users/bukan-uuid', { active: true }))).code).toBe('bad_request')
    expect((await failureOf(client.patch('/admin/users/50000000-0000-4000-8000-000000000099', { active: true }))).code).toBe('not_found')
  })

  it('menolak field asing dan tipe yang salah', async () => {
    const client = await asAdmin()
    expect((await failureOf(client.post('/admin/groups', { name: 'x', extra: 1 }))).code).toBe('bad_request')
    expect((await failureOf(client.post('/admin/users', { username: 'abc_def', password: strongPassword, platform_admin: 'ya' }))).code).toBe('bad_request')
  })
})

describe('user', () => {
  it('daftar terurut menurut username, dipaginasi, dan tanpa password', async () => {
    const client = await asAdmin()
    const first = await client.get<Page<Schemas['User']>>('/admin/users', { query: { limit: 3 } })
    expect(first.items.map((user) => user.username)).toEqual(['admin', 'auditor', 'editor'])
    expect(first.has_next).toBe(true)
    const last = await client.get<Page<Schemas['User']>>('/admin/users', { query: { page: 3, limit: 3 } })
    expect(last.items.map((user) => user.username)).toEqual(['reviewer'])
    expect(last.has_next).toBe(false)
    expect(JSON.stringify(first)).not.toContain(demoPassword)
  })

  it('membuat user baru yang langsung bisa login, dengan Location dan username ternormalisasi', async () => {
    const { client, me } = await signIn('admin')
    const user = await client.post<Schemas['User']>('/admin/users', { username: '  Siti_01 ', password: strongPassword })
    expect(user).toMatchObject({ username: 'siti_01', active: true, platform_admin: false })
    const res = await fetch('http://localhost/api/v1/admin/users', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': me.csrf_token },
      body: JSON.stringify({ username: 'budi_02', password: strongPassword }),
    })
    expect(res.status).toBe(201)
    expect(res.headers.get('Location')).toMatch(/^\/api\/v1\/admin\/users\/[0-9a-f-]{36}$/)
    await expect(newClient().post('/auth/login', { username: 'siti_01', password: strongPassword })).resolves.toBeTruthy()
  })

  it('validasi menandai username dan password per field', async () => {
    const client = await asAdmin()
    const err = await failureOf(client.post('/admin/users', { username: 'ab', password: 'pendek' }))
    expect(err.code).toBe('validation_failed')
    expect(err.fields.username).toHaveLength(1)
    expect(err.fields.password).toHaveLength(1)
  })

  it('menolak username ganda walau berbeda huruf besar', async () => {
    const client = await asAdmin()
    expect((await failureOf(client.post('/admin/users', { username: 'REVIEWER', password: strongPassword }))).code).toBe('conflict')
  })

  it('menonaktifkan user mencegah login berikutnya', async () => {
    const client = await asAdmin()
    const user = await client.patch<Schemas['User']>(`/admin/users/${ids.editor}`, { active: false })
    expect(user.active).toBe(false)
    expect((await failureOf(newClient().post('/auth/login', { username: 'editor', password: demoPassword }, { handleUnauthenticated: false }))).status).toBe(401)
  })

  it('tidak boleh menonaktifkan akun sendiri', async () => {
    const client = await asAdmin()
    const err = await failureOf(client.patch(`/admin/users/${ids.admin}`, { active: false }))
    expect(err.code).toBe('conflict')
    await expect(client.get('/auth/me')).resolves.toBeTruthy()
  })

  it('reset password memakai kebijakan password baru dan mencabut sesi pemiliknya', async () => {
    const client = await asAdmin()
    expect((await failureOf(client.patch(`/admin/users/${ids.editor}`, { password: 'pendek' }))).fields.password).toHaveLength(1)
    await client.patch(`/admin/users/${ids.editor}`, { password: strongPassword })
    await expect(newClient().post('/auth/login', { username: 'editor', password: strongPassword })).resolves.toBeTruthy()
    const self = await signIn('admin')
    await self.client.patch(`/admin/users/${ids.admin}`, { password: strongPassword })
    expect((await failureOf(self.client.get('/auth/me', { handleUnauthenticated: false }))).status).toBe(401)
  })

  it('patch kosong ditolak dengan 422', async () => {
    const client = await asAdmin()
    expect((await failureOf(client.patch(`/admin/users/${ids.editor}`, {}))).code).toBe('validation_failed')
  })
})

describe('group', () => {
  it('daftar membawa leader dan jumlah anggota', async () => {
    const client = await asAdmin()
    const page = await client.get<Page<Schemas['Group']>>('/admin/groups')
    expect(page.items.map((group) => group.name)).toEqual(['auditors', 'magang', 'readers', 'writers'])
    expect(page.items.find((group) => group.name === 'writers')).toMatchObject({ leader_user_id: ids.reviewer, member_count: 2 })
    expect(page.items.find((group) => group.name === 'magang')).toMatchObject({ leader_user_id: null, member_count: 0 })
  })

  it('membuat group tanpa leader dan menolak nama ganda atau tidak valid', async () => {
    const client = await asAdmin()
    const group = await client.post<Schemas['Group']>('/admin/groups', { name: '  tim-baru ' })
    expect(group).toMatchObject({ name: 'tim-baru', leader_user_id: null, member_count: 0 })
    expect((await failureOf(client.post('/admin/groups', { name: 'TIM-BARU' }))).code).toBe('conflict')
    expect((await failureOf(client.post('/admin/groups', { name: 'g'.repeat(65) }))).fields.name).toHaveLength(1)
    expect((await failureOf(client.post('/admin/groups', { name: '   ' }))).code).toBe('validation_failed')
  })

  it('mengganti nama dan menolak nama milik group lain', async () => {
    const client = await asAdmin()
    await expect(client.patch(`/admin/groups/${ids.readers}`, { name: 'pembaca' })).resolves.toMatchObject({ name: 'pembaca' })
    expect((await failureOf(client.patch(`/admin/groups/${ids.readers}`, { name: 'writers' }))).code).toBe('conflict')
    await expect(client.patch(`/admin/groups/${ids.readers}`, { name: 'PEMBACA' })).resolves.toMatchObject({ name: 'PEMBACA' })
  })

  it('menghapus group ikut menghapus akses miliknya', async () => {
    const client = await asAdmin()
    await expect(client.delete(`/admin/groups/${ids.writers}`)).resolves.toBeUndefined()
    const grants = await client.get<Page<Schemas['Grant']>>('/admin/grants')
    expect(grants.items.some((grant) => grant.group_id === ids.writers)).toBe(false)
    expect((await failureOf(client.delete(`/admin/groups/${ids.writers}`))).code).toBe('not_found')
  })
})

describe('anggota dan leader', () => {
  it('daftar anggota terurut dan dipaginasi', async () => {
    const client = await asAdmin()
    const page = await client.get<Page<Schemas['User']>>(`/admin/groups/${ids.writers}/members`, { query: { limit: 1 } })
    expect(page.items.map((user) => user.username)).toEqual(['editor'])
    expect(page.has_next).toBe(true)
  })

  it('menambah anggota bersifat idempoten dan mengabaikan user yang tidak ada', async () => {
    const client = await asAdmin()
    await client.put(`/admin/groups/${ids.magang}/members/${ids.pengamat}`)
    await client.put(`/admin/groups/${ids.magang}/members/${ids.pengamat}`)
    expect(getState().groups.find((group) => group.id === ids.magang)?.members).toEqual([ids.pengamat])
    expect((await failureOf(client.put(`/admin/groups/${ids.magang}/members/50000000-0000-4000-8000-000000000099`))).code).toBe('not_found')
  })

  it('menolak isi request pada operasi tanpa body', async () => {
    const { client } = await signIn('admin')
    expect((await failureOf(client.put(`/admin/groups/${ids.magang}/members/${ids.pengamat}`, {}))).code).toBe('bad_request')
  })

  it('mengeluarkan anggota biasa berhasil, leader ditolak, dan non-anggota 404', async () => {
    const client = await asAdmin()
    await expect(client.delete(`/admin/groups/${ids.writers}/members/${ids.editor}`)).resolves.toBeUndefined()
    const leader = await failureOf(client.delete(`/admin/groups/${ids.writers}/members/${ids.reviewer}`))
    expect(leader.code).toBe('conflict')
    expect((await failureOf(client.delete(`/admin/groups/${ids.writers}/members/${ids.editor}`))).code).toBe('not_found')
  })

  it('leader harus anggota aktif group', async () => {
    const client = await asAdmin()
    const outsider = await failureOf(client.put(`/admin/groups/${ids.writers}/leader`, { user_id: ids.observer }))
    expect(outsider.code).toBe('validation_failed')
    expect(outsider.fields.user_id).toHaveLength(1)
    getState().users.find((user) => user.id === ids.editor)!.active = false
    expect((await failureOf(client.put(`/admin/groups/${ids.writers}/leader`, { user_id: ids.editor }))).fields.user_id).toHaveLength(1)
    expect((await failureOf(client.put(`/admin/groups/${ids.writers}/leader`, {}))).code).toBe('validation_failed')
  })

  it('mengganti leader membuat user itu memimpin group saat login', async () => {
    const client = await asAdmin()
    await client.put(`/admin/groups/${ids.magang}/members/${ids.pengamat}`)
    await expect(client.put(`/admin/groups/${ids.magang}/leader`, { user_id: ids.pengamat })).resolves.toMatchObject({ leader_user_id: ids.pengamat, member_count: 1 })
    const { me } = await signIn('pengamat')
    expect(me.leader_of).toEqual([ids.magang])
  })
})

describe('koneksi', () => {
  it('daftar terurut dan tidak pernah memuat DSN', async () => {
    const client = await asAdmin()
    const page = await client.get<Page<Schemas['Connection']>>('/admin/connections')
    expect(page.items.map((connection) => connection.name)).toEqual(['arsip', 'inventaris', 'pelaporan'])
    expect(page.items[0]).toEqual({ id: ids.arsip, name: 'arsip', engine: 'mysql' })
  })

  it('membuat koneksi tanpa mengembalikan DSN', async () => {
    const client = await asAdmin()
    const connection = await client.post<Schemas['Connection']>('/admin/connections', { name: 'baru', engine: 'postgresql', dsn: 'postgres://u:rahasia-banget@h/db' })
    expect(connection).toEqual({ id: expect.any(String), name: 'baru', engine: 'postgresql' })
    const list = await client.get('/admin/connections')
    expect(JSON.stringify(list)).not.toContain('rahasia-banget')
  })

  it('validasi menandai engine, DSN, dan nama, serta menolak nama ganda', async () => {
    const client = await asAdmin()
    const err = await failureOf(client.post('/admin/connections', { name: '', engine: 'oracle', dsn: '' }))
    expect(err.fields.name).toHaveLength(1)
    expect(err.fields.engine).toHaveLength(1)
    expect(err.fields.dsn).toHaveLength(1)
    expect((await failureOf(client.post('/admin/connections', { name: 'x', engine: 'postgresql', dsn: 'host=db' }))).fields.dsn).toHaveLength(1)
    expect((await failureOf(client.post('/admin/connections', { name: 'INVENTARIS', engine: 'postgresql', dsn: 'postgres://u:p@h/db' }))).code).toBe('conflict')
  })

  it('menghapus koneksi ikut menghapus akses ke koneksi itu', async () => {
    const client = await asAdmin()
    await client.delete(`/admin/connections/${ids.arsip}`)
    const grants = await client.get<Page<Schemas['Grant']>>('/admin/grants')
    expect(grants.items.some((grant) => grant.database_id === ids.arsip)).toBe(false)
    expect((await failureOf(client.delete(`/admin/connections/${ids.arsip}`))).code).toBe('not_found')
  })
})

describe('akses database', () => {
  it('daftar terurut menurut group lalu database dan membawa nama', async () => {
    const client = await asAdmin()
    const page = await client.get<Page<Schemas['Grant']>>('/admin/grants')
    expect(page.items).toHaveLength(6)
    expect(page.items[0]).toEqual({ group_id: ids.writers, group_name: 'writers', database_id: ids.inventaris, database_name: 'inventaris', access: 'view_edit' })
    const keys = page.items.map((grant) => `${grant.group_id}/${grant.database_id}`)
    expect(keys).toEqual([...keys].sort())
  })

  it('memberi akses baru lalu mengubah levelnya tanpa menggandakan', async () => {
    const client = await asAdmin()
    await expect(client.put(`/admin/grants/${ids.magang}/${ids.arsip}`, { access: 'view_only' })).resolves.toMatchObject({ group_name: 'magang', database_name: 'arsip', access: 'view_only' })
    await client.put(`/admin/grants/${ids.magang}/${ids.arsip}`, { access: 'view_edit' })
    const grants = getState().grants.filter((grant) => grant.group_id === ids.magang && grant.database_id === ids.arsip)
    expect(grants).toEqual([{ group_id: ids.magang, database_id: ids.arsip, access: 'view_edit' }])
  })

  it('menolak level yang tidak dikenal dan group atau database yang tidak ada', async () => {
    const client = await asAdmin()
    expect((await failureOf(client.put(`/admin/grants/${ids.magang}/${ids.arsip}`, { access: 'none' }))).fields.access).toHaveLength(1)
    expect((await failureOf(client.put(`/admin/grants/50000000-0000-4000-8000-000000000099/${ids.arsip}`, { access: 'view_only' }))).code).toBe('not_found')
    expect((await failureOf(client.put(`/admin/grants/${ids.magang}/50000000-0000-4000-8000-000000000099`, { access: 'view_only' }))).code).toBe('not_found')
  })

  it('mencabut akses, dan mencabut yang tidak ada adalah 404', async () => {
    const client = await asAdmin()
    await expect(client.delete(`/admin/grants/${ids.magang}/${ids.pelaporan}`)).resolves.toBeUndefined()
    expect((await failureOf(client.delete(`/admin/grants/${ids.magang}/${ids.pelaporan}`))).code).toBe('not_found')
  })
})
