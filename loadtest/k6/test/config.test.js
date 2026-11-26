import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { loadConfig } from '../lib/config.js'

describe('loadConfig', () => {
  it('memberi nilai bawaan untuk satu mesin dan profil stres', () => {
    const config = loadConfig({})
    assert.equal(config.profile, 'stress')
    assert.equal(config.baseUrl, 'http://localhost:8080')
    assert.equal(config.origin, 'http://localhost:8080')
    assert.equal(config.readVus, 10)
    assert.equal(config.jobVus, 2)
    assert.deepEqual(config.multipliers, [1, 2, 4, 8, 1])
    assert.deepEqual(config.leaders, ['reviewer', 'observer'])
    assert.equal(config.readP95Ms, 500)
    assert.equal(config.maxErrorRate, 0.01)
  })

  it('Origin diturunkan dari alamat dasar tanpa path dan garis miring penutup', () => {
    const config = loadConfig({ BASE_URL: 'https://uji.example:9443/aplikasi///' })
    assert.equal(config.baseUrl, 'https://uji.example:9443/aplikasi')
    assert.equal(config.origin, 'https://uji.example:9443')
    assert.equal(loadConfig({ BASE_URL: 'http://x', ORIGIN: 'http://localhost:5173' }).origin, 'http://localhost:5173')
  })

  it('membaca angka dan daftar dari variabel lingkungan', () => {
    const config = loadConfig({ READ_VUS: '25', JOB_VUS: '4', MULTIPLIERS: '1, 3, 1', LEADER_USERS: 'a,b', DATABASES: 'inventaris, pelaporan', SAME_DB: 'true', PROFILE: 'smoke' })
    assert.equal(config.readVus, 25)
    assert.equal(config.jobVus, 4)
    assert.deepEqual(config.multipliers, [1, 3, 1])
    assert.deepEqual(config.leaders, ['a', 'b'])
    assert.deepEqual(config.databases, ['inventaris', 'pelaporan'])
    assert.equal(config.sameDb, true)
    assert.equal(config.profile, 'smoke')
  })

  it('nilai yang bukan angka atau tidak masuk akal kembali ke bawaan atau batas minimum', () => {
    const config = loadConfig({ READ_VUS: 'banyak', JOB_VUS: '0', MULTIPLIERS: 'x,-2,0', RAMP_SECONDS: '', POLL_SECONDS: '0', JOB_TIMEOUT_SECONDS: '1' })
    assert.equal(config.readVus, 10)
    assert.equal(config.jobVus, 1)
    assert.deepEqual(config.multipliers, [1, 2, 4, 8, 1])
    assert.equal(config.rampSeconds, 30)
    assert.equal(config.pollSeconds, 0.1)
    assert.equal(config.jobTimeoutSeconds, 5)
  })

  it('semua scenario dijalankan secara bawaan dan SCENARIOS memilih sebagian dengan nama tak dikenal diabaikan', () => {
    assert.deepEqual(loadConfig({}).scenarios, ['reads', 'jobs'])
    assert.deepEqual(loadConfig({ SCENARIOS: 'reads' }).scenarios, ['reads'])
    assert.deepEqual(loadConfig({ SCENARIOS: ' jobs , jobs ' }).scenarios, ['jobs'])
    assert.deepEqual(loadConfig({ SCENARIOS: 'lainnya' }).scenarios, ['reads', 'jobs'])
  })

  it('endpoint workflow ikut dibaca kecuali READ_WORKFLOW bernilai false', () => {
    assert.equal(loadConfig({}).readWorkflow, true)
    assert.equal(loadConfig({ READ_WORKFLOW: 'false' }).readWorkflow, false)
    assert.equal(loadConfig({ READ_WORKFLOW: 'true' }).readWorkflow, true)
  })

  it('kata sandi diambil per akun lebih dulu, lalu satu kata sandi bersama, lalu nilai demo', () => {
    const config = loadConfig({ K6_PASSWORD: 'bersama', K6_PASSWORD_EDITOR: 'milik-editor', K6_PASSWORD_PENGAMAT_2: 'angka' })
    assert.equal(config.passwordFor('editor'), 'milik-editor')
    assert.equal(config.passwordFor('reviewer'), 'bersama')
    assert.equal(config.passwordFor('pengamat-2'), 'angka')
    assert.equal(loadConfig({}).passwordFor('editor'), 'tocatdz-demo-2026')
    assert.equal(loadConfig({ K6_PASSWORD_EDITOR: '' }).passwordFor('editor'), 'tocatdz-demo-2026')
  })

  it('jumlah sesi per akun bernilai 4 secara bawaan dan dibatasi antara 1 dan 5', () => {
    assert.equal(loadConfig({}).sessionsPerAccount, 4)
    assert.equal(loadConfig({ SESSIONS_PER_ACCOUNT: '9' }).sessionsPerAccount, 5)
    assert.equal(loadConfig({ SESSIONS_PER_ACCOUNT: '0' }).sessionsPerAccount, 1)
    assert.equal(loadConfig({ SESSIONS_PER_ACCOUNT: '2' }).sessionsPerAccount, 2)
  })

  it('profil selain smoke dianggap stres', () => {
    assert.equal(loadConfig({ PROFILE: 'apa-saja' }).profile, 'stress')
  })
})
