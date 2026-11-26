import assert from 'node:assert/strict'
import { register } from 'node:module'
import { beforeEach, describe, it } from 'node:test'
import { createBackend } from './fake-backend.js'

register('./stubs/loader.mjs', import.meta.url)

let counter = 0

// Setiap test memuat stress.js dari awal karena konfigurasi dibaca dari __ENV saat modul dimuat
async function load(env = {}, backendOptions = {}) {
  const backend = createBackend(backendOptions)
  globalThis.__ENV = { K6_PASSWORD: 'pw', ...env }
  globalThis.__k6 = { backend, checks: [], metrics: {}, execution: { vu: { idInTest: 1 }, scenario: { startTime: Date.now(), iterationInTest: 0 } } }
  const module = await import(`../stress.js?kasus=${counter++}`)
  return { module, backend, k6: globalThis.__k6 }
}

describe('stress.js', () => {
  beforeEach(() => {
    delete globalThis.__k6
  })

  it('mendefinisikan dua scenario paralel untuk baca dan job dengan tahapan naik lalu turun', async () => {
    const { module } = await load({ READ_VUS: '10', JOB_VUS: '2' })
    const { scenarios } = module.options
    assert.equal(scenarios.reads.exec, 'reads')
    assert.equal(scenarios.jobs.exec, 'jobs')
    assert.equal(scenarios.reads.executor, 'ramping-vus')
    assert.deepEqual(scenarios.reads.stages.map((stage) => stage.target), [10, 10, 20, 20, 40, 40, 80, 80, 10, 10])
    assert.deepEqual(scenarios.jobs.stages.map((stage) => stage.target), [2, 2, 4, 4, 8, 8, 16, 16, 2, 2])
    assert.equal(typeof module.reads, 'function')
    assert.equal(typeof module.jobs, 'function')
    assert.equal(typeof module.setup, 'function')
  })

  it('menunggu job selesai memakai batas yang sama untuk berhenti dengan rapi', async () => {
    const { module } = await load({ JOB_TIMEOUT_SECONDS: '90' })
    assert.equal(module.options.scenarios.jobs.gracefulStop, '90s')
    assert.equal(module.options.scenarios.jobs.gracefulRampDown, '90s')
  })

  it('profil smoke memakai satu VU singkat dan ringkasan menyertakan median serta p95', async () => {
    const { module } = await load({ PROFILE: 'smoke' })
    assert.deepEqual(module.options.scenarios.reads.stages, [{ duration: '20s', target: 1 }])
    assert.ok(module.options.summaryTrendStats.includes('med'))
    assert.ok(module.options.summaryTrendStats.includes('p(95)'))
  })

  it('ambang batas utama dan ambang per tahap tersedia', async () => {
    const { module } = await load()
    assert.deepEqual(module.options.thresholds['http_req_duration{kind:read}'], ['p(95)<500'])
    assert.ok('http_req_duration{kind:read,stage:tahap3-x4}' in module.options.thresholds)
  })

  it('metrik custom terdaftar dengan nama yang dipakai di laporan', async () => {
    const { k6 } = await load()
    for (const name of ['jobs_completed', 'jobs_failed', 'jobs_timed_out', 'job_queue_wait_ms', 'job_duration_ms', 'job_e2e_ms', 'submit_conflicts', 'job_success']) {
      assert.ok(name in k6.metrics, name)
    }
    assert.equal(k6.metrics.job_queue_wait_ms.isTime, true)
    assert.equal(k6.metrics.job_success.type, 'rate')
    assert.equal(k6.metrics.jobs_completed.type, 'counter')
  })

  it('setup memilih database PostgreSQL yang bisa diubah dan membawa satu tabel untuk pembacaan', async () => {
    const { module } = await load()
    const data = module.setup()
    assert.deepEqual(data.databases.map((database) => database.name), ['inventaris', 'pelaporan'])
    assert.deepEqual(data.databases[0].table, { schema: 'public', name: 'orders' })
    assert.equal(typeof data.runId, 'string')
  })

  it('DB_COUNT dan DATABASES membatasi pilihan, dan SAME_DB hanya memakai satu database', async () => {
    assert.equal((await load({ DB_COUNT: '1' })).module.setup().databases.length, 1)
    assert.deepEqual((await load({ DATABASES: 'pelaporan' })).module.setup().databases.map((database) => database.name), ['pelaporan'])
    assert.equal((await load({ SAME_DB: 'true' })).module.setup().databases.length, 1)
  })

  it('setup gagal dengan pesan jelas bila akun salah atau tidak ada database yang cocok', async () => {
    const wrongPassword = await load({ K6_PASSWORD: 'salah' })
    assert.throws(() => wrongPassword.module.setup(), /login editor gagal/)
    const missing = await load({ DATABASES: 'tidak-ada' })
    assert.throws(() => missing.module.setup(), /tidak ada database PostgreSQL/)
    const leader = await load({ LEADER_USERS: 'hantu' })
    assert.throws(() => leader.module.setup(), /login leader hantu gagal/)
  })

  it('scenario baca mengirim satu permintaan baca bertag dan tidur sebentar', async () => {
    const { module, backend, k6 } = await load()
    const data = module.setup()
    const before = backend.state.log.length
    const clock = backend.clock.t
    module.reads(data)
    const entries = backend.state.log.slice(before).filter((entry) => entry.tags?.kind === 'read')
    assert.equal(entries.length, 1)
    assert.equal(entries[0].tags.stage, 'tahap1-x1')
    assert.ok(backend.clock.t > clock)
    assert.ok(k6.checks.length >= 1 && k6.checks.every(([, ok]) => ok))
  })

  it('scenario job menjalankan alur lengkap dengan tabel unik per VU dan iterasi lalu mencatat hasilnya', async () => {
    const { module, backend, k6 } = await load({}, { runMs: 2000, queueMs: 500 })
    const data = module.setup()
    k6.execution.vu.idInTest = 3
    k6.execution.scenario.iterationInTest = 7
    module.jobs(data)
    assert.match(backend.state.migrations[0].up_sql, new RegExp(`k6_${data.runId}_3_7`))
    assert.equal(k6.metrics.jobs_completed.values.length, 1)
    assert.equal(k6.metrics.job_queue_wait_ms.values[0].value, 500)
    assert.equal(k6.metrics.job_duration_ms.values[0].value, 2000)
  })

  it('VU berbeda dipetakan ke database berbeda kecuali SAME_DB aktif', async () => {
    const spread = await load()
    const data = spread.module.setup()
    spread.k6.execution.vu.idInTest = 1
    spread.module.jobs(data)
    spread.k6.execution.vu.idInTest = 2
    spread.module.jobs(data)
    assert.deepEqual(spread.backend.state.migrations.map((migration) => migration.database_id), ['db-1', 'db-2'])
    const same = await load({ SAME_DB: 'true' })
    const sameData = same.module.setup()
    same.k6.execution.vu.idInTest = 2
    same.module.jobs(sameData)
    assert.deepEqual(same.backend.state.migrations.map((migration) => migration.database_id), ['db-1'])
  })

  it('setiap VU login sekali per akun dan memakai ulang sesinya antar iterasi', async () => {
    const { module, backend } = await load({}, { runMs: 1000, queueMs: 200 })
    const data = module.setup()
    const afterSetup = backend.state.logins
    module.jobs(data)
    const afterFirst = backend.state.logins
    module.jobs(data)
    assert.equal(afterFirst - afterSetup, 3)
    assert.equal(backend.state.logins, afterFirst)
  })
})
