import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { createClient } from '../lib/client.js'
import { pickTarget, readOnce, readTargets } from '../lib/reads.js'
import { createBackend } from './fake-backend.js'

const database = { id: 'db-1', name: 'inventaris', table: { schema: 'public', name: 'orders' } }

describe('endpoint baca', () => {
  it('mencakup daftar database, schema, posisi, riwayat dengan dan tanpa hide_rollback, dan baris tabel', () => {
    const names = readTargets(database).map((target) => target.name)
    for (const expected of ['GET /databases', 'GET /databases/{id}/schema', 'GET /databases/{id}/position', 'GET /databases/{id}/executions', 'GET /databases/{id}/executions hide_rollback', 'GET /databases/{id}/tables/{schema}/{table}/rows']) {
      assert.ok(names.includes(expected), expected)
    }
  })

  it('nama endpoint tidak memuat ID dan setiap path memuat ID database', () => {
    for (const target of readTargets(database)) {
      assert.doesNotMatch(target.name, /db-1/)
      if (target.name !== 'GET /databases') assert.match(target.path, /\/databases\/db-1\//)
    }
  })

  it('endpoint baris tabel hanya ada bila database punya tabel dan nama tabel di-encode', () => {
    assert.equal(readTargets({ ...database, table: null }).length, readTargets(database).length - 1)
    const target = readTargets({ ...database, table: { schema: 'sch ema', name: 'a/b' } }).at(-1)
    assert.match(target.path, /tables\/sch%20ema\/a%2Fb\/rows/)
  })

  it('tanpa workflow hanya endpoint baca dasar yang tersisa dan semuanya bukan endpoint workflow', () => {
    const names = readTargets(database, { workflow: false }).map((target) => target.name)
    assert.deepEqual(names, ['GET /databases', 'GET /databases/{id}/schema', 'GET /databases/{id}/tables/{schema}/{table}/rows'])
    assert.equal(readTargets(database, { workflow: true }).length, readTargets(database).length)
  })

  it('pemilihan acak mencakup seluruh endpoint dan tidak keluar rentang', () => {
    const targets = readTargets(database)
    const picked = new Set()
    for (let index = 0; index < targets.length; index++) picked.add(pickTarget(database, () => index / targets.length).name)
    assert.equal(picked.size, targets.length)
    assert.equal(pickTarget(database, () => 0.9999999).name, targets.at(-1).name)
    assert.equal(pickTarget(database, () => 0).name, targets[0].name)
  })
})

describe('satu pembacaan', () => {
  it('tanpa workflow tidak pernah meminta endpoint workflow berapa pun nilai acaknya', () => {
    const backend = createBackend()
    const client = createClient({ http: backend.http, baseUrl: 'http://localhost:8080', origin: 'http://localhost:8080' })
    const session = client.newSession('editor', 'pw')
    client.login(session)
    for (let index = 0; index < 50; index++) readOnce({ client, session, database, stage: () => 'tahap1-x1', random: () => index / 50, check: () => undefined, workflow: false })
    const paths = backend.state.log.filter((entry) => entry.tags?.kind === 'read').map((entry) => entry.path)
    assert.equal(paths.length, 50)
    for (const path of paths) assert.doesNotMatch(path, /position|executions|migrations|change-requests|jobs/)
  })

  it('diberi tag jenis baca dan tahap, dan memeriksa status 200', () => {
    const backend = createBackend()
    const client = createClient({ http: backend.http, baseUrl: 'http://localhost:8080', origin: 'http://localhost:8080' })
    const session = client.newSession('editor', 'pw')
    client.login(session)
    const checks = []
    const res = readOnce({ client, session, database, stage: () => 'tahap2-x2', random: () => 0.2, check: (response, rules) => checks.push([response.status, Object.keys(rules)]) })
    assert.equal(res.status, 200)
    const entry = backend.state.log.at(-1)
    assert.equal(entry.tags.kind, 'read')
    assert.equal(entry.tags.stage, 'tahap2-x2')
    assert.equal(checks.length, 1)
    assert.match(checks[0][1][0], / 200$/)
  })
})
