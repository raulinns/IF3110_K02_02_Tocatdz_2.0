import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { createClient } from '../lib/client.js'
import { createBackend } from './fake-backend.js'

const setup = (options) => {
  const backend = createBackend(options)
  const client = createClient({ http: backend.http, baseUrl: 'http://localhost:8080', origin: 'http://localhost:8080' })
  return { backend, client }
}

describe('klien API', () => {
  it('login menyimpan token CSRF dan user di sesi dan mengirim header Origin', () => {
    const { backend, client } = setup()
    const session = client.newSession('editor', 'pw')
    const res = client.login(session)
    assert.equal(res.status, 200)
    assert.equal(session.csrf.startsWith('csrf-'), true)
    assert.equal(session.user.id, 'u-editor')
    assert.equal(backend.state.log[0].headers.Origin, 'http://localhost:8080')
    assert.equal(backend.state.log[0].headers['Content-Type'], 'application/json')
  })

  it('login yang salah tidak menyimpan token dan 401 dinyatakan wajar pada permintaannya', () => {
    const { backend, client } = setup()
    const session = client.newSession('editor', 'salah')
    assert.equal(client.login(session).status, 401)
    assert.equal(session.csrf, null)
    assert.deepEqual(backend.state.log[0].expected, [200, 401])
  })

  it('token CSRF hanya dikirim pada mutasi dan tidak pada pembacaan', () => {
    const { backend, client } = setup()
    const session = client.newSession('editor', 'pw')
    client.login(session)
    client.request(session, 'GET', '/databases')
    client.request(session, 'POST', '/databases/db-1/migrations', { body: { title: 'x', up_sql: 'a', down_sql: 'b', mode: 'normal', batch: null } })
    const [, read, write] = backend.state.log
    assert.equal(read.headers['X-CSRF-Token'], undefined)
    assert.equal(write.headers['X-CSRF-Token'], session.csrf)
  })

  it('setiap akun memakai cookie jar sendiri sehingga sesinya tidak bertukar', () => {
    const { client, backend } = setup()
    const editor = client.newSession('editor', 'pw')
    const reviewer = client.newSession('reviewer', 'pw')
    client.login(editor)
    client.login(reviewer)
    assert.notEqual(editor.jar.cookies.sid, reviewer.jar.cookies.sid)
    assert.equal(client.request(editor, 'GET', '/auth/me').json().user.username, 'editor')
    assert.equal(client.request(reviewer, 'GET', '/auth/me').json().user.username, 'reviewer')
    assert.equal(backend.state.logins, 2)
  })

  it('sesi yang berakhir diperbaiki dengan login ulang lalu permintaan diulang satu kali', () => {
    const { client, backend } = setup({ expireFirstSession: true })
    const session = client.newSession('editor', 'pw')
    client.login(session)
    const res = client.request(session, 'GET', '/databases')
    assert.equal(res.status, 200)
    assert.equal(backend.state.logins, 2)
  })

  it('nama permintaan diberikan lewat tag dan tag tambahan ikut terbawa', () => {
    const { client, backend } = setup()
    const session = client.newSession('editor', 'pw')
    client.login(session)
    client.request(session, 'GET', '/databases/db-1/position', { name: 'GET /databases/{id}/position', tags: { kind: 'read', stage: 'tahap1-x1' } })
    assert.deepEqual(backend.state.log.at(-1).tags, { name: 'GET /databases/{id}/position', kind: 'read', stage: 'tahap1-x1' })
  })

  it('status yang diharapkan diteruskan sebagai callback respons k6', () => {
    const { client, backend } = setup()
    const session = client.newSession('editor', 'pw')
    client.login(session)
    client.request(session, 'GET', '/databases', { expected: [200, 409] })
    assert.deepEqual(backend.state.log.at(-1).expected, [200, 409])
  })

  it('tanpa login permintaan ditolak dengan 401 dan upaya login ulang dengan akun salah tetap 401', () => {
    const { client } = setup()
    const session = client.newSession('editor', 'salah')
    assert.equal(client.request(session, 'GET', '/databases').status, 401)
  })
})
