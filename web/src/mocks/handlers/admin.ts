import { http } from 'msw/http'
import type { Schemas } from '../../api/types.ts'
import { ApiFailure, apiPrefix, booleanField, conflict, noContent, notFound, ok, pageOf, pagingKeys, readJson, readPaging, requireNoBody, route, secured, sortBy, stringField } from '../http.ts'
import { toConnection, toGroup, toUser } from '../models.ts'
import { getState, setSession, type MockConnection, type MockGrant, type MockGroup, type MockUser } from '../state.ts'
import { dsnError, Fields, nameError, normalizeUsername, passwordError, usernameError } from '../validation.ts'

const adminOnly = { admin: true }
const adminPaged = { admin: true, query: pagingKeys }
const sameName = (a: string, b: string) => a.toLowerCase() === b.toLowerCase()

function findGroup(id: string): MockGroup {
  const group = getState().groups.find((candidate) => candidate.id === id)
  if (!group) throw notFound()
  return group
}

function findUser(id: string): MockUser {
  const user = getState().users.find((candidate) => candidate.id === id)
  if (!user) throw notFound()
  return user
}

function findConnection(id: string): MockConnection {
  const connection = getState().connections.find((candidate) => candidate.id === id)
  if (!connection) throw notFound()
  return connection
}

function toGrant(grant: MockGrant): Schemas['Grant'] {
  const state = getState()
  return {
    group_id: grant.group_id,
    group_name: state.groups.find((group) => group.id === grant.group_id)?.name ?? '',
    database_id: grant.database_id,
    database_name: state.connections.find((connection) => connection.id === grant.database_id)?.name ?? '',
    access: grant.access,
  }
}

const created = (body: unknown, collection: string, id: string) => ok(body, 201, { Location: `${apiPrefix}/admin/${collection}/${id}` })

export const adminHandlers = [
  http.get(
    route('/admin/users'),
    secured(({ url }) => {
      const users = [...getState().users].sort(sortBy<MockUser>((user) => user.username, (user) => user.id))
      return ok(pageOf(users.map(toUser), readPaging(url)))
    }, adminPaged),
  ),

  http.post(
    route('/admin/users'),
    secured(async ({ request }) => {
      const body = await readJson(request, ['username', 'password', 'platform_admin'])
      const username = normalizeUsername(stringField(body, 'username') ?? '')
      const password = stringField(body, 'password') ?? ''
      const platformAdmin = booleanField(body, 'platform_admin') ?? false
      const fields = new Fields()
      fields.add('username', usernameError(username))
      fields.add('password', passwordError(password))
      fields.check()
      const state = getState()
      if (state.users.some((user) => user.username === username)) throw conflict('Username sudah dipakai')
      const user: MockUser = { id: crypto.randomUUID(), username, password, active: true, platform_admin: platformAdmin }
      state.users.push(user)
      return created(toUser(user), 'users', user.id)
    }, adminOnly),
  ),

  // Mengubah status aktif atau mereset password, keduanya mencabut sesi milik user yang diubah
  http.patch(
    route('/admin/users/:user_id'),
    secured(async ({ request, params, user, session }) => {
      const body = await readJson(request, ['active', 'password'])
      const active = booleanField(body, 'active')
      const password = stringField(body, 'password')
      const target = findUser(params.user_id)
      if (active === undefined && password === undefined) throw new ApiFailure(422, 'validation_failed', 'Input tidak valid')
      const fields = new Fields()
      if (password !== undefined) fields.add('password', passwordError(password))
      fields.check()
      if (active === false && target.id === user.id) throw conflict('Tidak dapat menonaktifkan akun sendiri')
      if (active !== undefined) target.active = active
      if (password !== undefined) target.password = password
      if (session.userId === target.id && (active === false || password !== undefined)) setSession(null)
      return ok(toUser(target))
    }, adminOnly),
  ),

  http.get(
    route('/admin/groups'),
    secured(({ url }) => {
      const groups = [...getState().groups].sort(sortBy<MockGroup>((group) => group.name, (group) => group.id))
      return ok(pageOf(groups.map(toGroup), readPaging(url)))
    }, adminPaged),
  ),

  http.post(
    route('/admin/groups'),
    secured(async ({ request }) => {
      const body = await readJson(request, ['name'])
      const name = (stringField(body, 'name') ?? '').trim()
      const fields = new Fields()
      fields.add('name', nameError(name, 'Nama group'))
      fields.check()
      const state = getState()
      if (state.groups.some((group) => sameName(group.name, name))) throw conflict('Nama group sudah dipakai')
      const group: MockGroup = { id: crypto.randomUUID(), name, leader_user_id: null, members: [] }
      state.groups.push(group)
      return created(toGroup(group), 'groups', group.id)
    }, adminOnly),
  ),

  http.patch(
    route('/admin/groups/:group_id'),
    secured(async ({ request, params }) => {
      const body = await readJson(request, ['name'])
      const name = (stringField(body, 'name') ?? '').trim()
      const group = findGroup(params.group_id)
      const fields = new Fields()
      fields.add('name', nameError(name, 'Nama group'))
      fields.check()
      if (getState().groups.some((other) => other.id !== group.id && sameName(other.name, name))) throw conflict('Nama group sudah dipakai')
      group.name = name
      return ok(toGroup(group))
    }, adminOnly),
  ),

  // Menghapus group ikut menghapus keanggotaan dan aksesnya seperti ON DELETE CASCADE di M1
  http.delete(
    route('/admin/groups/:group_id'),
    secured(async ({ request, params }) => {
      await requireNoBody(request)
      const group = findGroup(params.group_id)
      const state = getState()
      state.groups = state.groups.filter((candidate) => candidate.id !== group.id)
      state.grants = state.grants.filter((grant) => grant.group_id !== group.id)
      return noContent()
    }, adminOnly),
  ),

  http.get(
    route('/admin/connections'),
    secured(({ url }) => {
      const connections = [...getState().connections].sort(sortBy<MockConnection>((connection) => connection.name, (connection) => connection.id))
      return ok(pageOf(connections.map(toConnection), readPaging(url)))
    }, adminPaged),
  ),

  // DSN hanya divalidasi dan tidak pernah disimpan atau dikembalikan
  http.post(
    route('/admin/connections'),
    secured(async ({ request }) => {
      const body = await readJson(request, ['name', 'engine', 'dsn'])
      const name = (stringField(body, 'name') ?? '').trim()
      const engine = stringField(body, 'engine')
      const dsn = stringField(body, 'dsn') ?? ''
      const fields = new Fields()
      fields.add('name', nameError(name, 'Nama koneksi'))
      fields.add('engine', engine === 'postgresql' || engine === 'mysql' ? null : 'Engine harus postgresql atau mysql')
      fields.add('dsn', dsnError(dsn, engine))
      fields.check()
      const state = getState()
      if (state.connections.some((connection) => sameName(connection.name, name))) throw conflict('Nama koneksi sudah dipakai')
      const connection: MockConnection = { id: crypto.randomUUID(), name, engine: engine as MockConnection['engine'] }
      state.connections.push(connection)
      return created(toConnection(connection), 'connections', connection.id)
    }, adminOnly),
  ),

  http.delete(
    route('/admin/connections/:connection_id'),
    secured(async ({ request, params }) => {
      await requireNoBody(request)
      const connection = findConnection(params.connection_id)
      const state = getState()
      state.connections = state.connections.filter((candidate) => candidate.id !== connection.id)
      state.grants = state.grants.filter((grant) => grant.database_id !== connection.id)
      state.tables = state.tables.filter((table) => table.database_id !== connection.id)
      return noContent()
    }, adminOnly),
  ),

  http.get(
    route('/admin/groups/:group_id/members'),
    secured(({ url, params }) => {
      const group = findGroup(params.group_id)
      const members = getState()
        .users.filter((user) => group.members.includes(user.id))
        .sort(sortBy<MockUser>((user) => user.username, (user) => user.id))
      return ok(pageOf(members.map(toUser), readPaging(url)))
    }, adminPaged),
  ),

  http.put(
    route('/admin/groups/:group_id/members/:user_id'),
    secured(async ({ request, params }) => {
      await requireNoBody(request)
      const group = findGroup(params.group_id)
      const user = findUser(params.user_id)
      if (!group.members.includes(user.id)) group.members.push(user.id)
      return noContent()
    }, adminOnly),
  ),

  http.delete(
    route('/admin/groups/:group_id/members/:user_id'),
    secured(async ({ request, params }) => {
      await requireNoBody(request)
      const group = findGroup(params.group_id)
      if (!group.members.includes(params.user_id)) throw notFound()
      if (group.leader_user_id === params.user_id) throw conflict('Leader tidak dapat dikeluarkan dari group, ganti leader terlebih dahulu')
      group.members = group.members.filter((id) => id !== params.user_id)
      return noContent()
    }, adminOnly),
  ),

  http.put(
    route('/admin/groups/:group_id/leader'),
    secured(async ({ request, params }) => {
      const body = await readJson(request, ['user_id'])
      const userId = stringField(body, 'user_id') ?? ''
      const group = findGroup(params.group_id)
      const candidate = getState().users.find((user) => user.id === userId)
      const fields = new Fields()
      fields.add('user_id', candidate?.active && group.members.includes(candidate.id) ? null : 'Leader harus anggota aktif group')
      fields.check()
      group.leader_user_id = userId
      return ok(toGroup(group))
    }, adminOnly),
  ),

  http.get(
    route('/admin/grants'),
    secured(({ url }) => {
      const grants = [...getState().grants].sort(sortBy<MockGrant>((grant) => grant.group_id, (grant) => grant.database_id))
      return ok(pageOf(grants.map(toGrant), readPaging(url)))
    }, adminPaged),
  ),

  http.put(
    route('/admin/grants/:group_id/:database_id'),
    secured(async ({ request, params }) => {
      const body = await readJson(request, ['access'])
      const access = stringField(body, 'access')
      const group = findGroup(params.group_id)
      const connection = findConnection(params.database_id)
      const fields = new Fields()
      fields.add('access', access === 'view_only' || access === 'view_edit' ? null : 'Akses harus view_only atau view_edit')
      fields.check()
      const level = access as MockGrant['access']
      const state = getState()
      const existing = state.grants.find((grant) => grant.group_id === group.id && grant.database_id === connection.id)
      if (existing) existing.access = level
      else state.grants.push({ group_id: group.id, database_id: connection.id, access: level })
      return ok(toGrant({ group_id: group.id, database_id: connection.id, access: level }))
    }, adminOnly),
  ),

  http.delete(
    route('/admin/grants/:group_id/:database_id'),
    secured(async ({ request, params }) => {
      await requireNoBody(request)
      const state = getState()
      const index = state.grants.findIndex((grant) => grant.group_id === params.group_id && grant.database_id === params.database_id)
      if (index < 0) throw notFound()
      state.grants.splice(index, 1)
      return noContent()
    }, adminOnly),
  ),
]
