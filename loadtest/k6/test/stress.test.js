import assert from 'node:assert/strict'
import { register } from 'node:module'
import { beforeEach, describe, it } from 'node:test'
import { createBackend } from './fake-backend.js'

register('./stubs/loader.mjs', import.meta.url)

let counter = 0

// Setiap test memuat stress.js dari awal karena konfigurasi dibaca dari __ENV saat modul dimuat
// Backend bisa dipakai bersama supaya beberapa VU, yang di k6 adalah instance modul terpisah, berhadapan dengan server yang sama
async function load(env = {}, backendOptions = {}, shared = undefined) {
  const backend = shared ?? createBackend(backendOptions)
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

  it('VU memakai sesi hasil login setup() dan tidak login lagi sehingga pembatas login per akun tidak tersentuh', async () => {
    const { module, backend, k6 } = await load({}, { runMs: 1000, queueMs: 200 })
    const data = module.setup()
    assert.equal(backend.state.logins, 12)
    assert.deepEqual(Object.keys(data.sessions).sort(), ['editor', 'observer', 'reviewer'])
    for (const pool of Object.values(data.sessions)) assert.equal(pool.length, 4)
    module.jobs(data)
    module.jobs(data)
    module.reads(data)
    assert.equal(backend.state.logins, 12)
    assert.equal(k6.metrics.jobs_completed.values.length, 2)
  })

  it('sesi di data setup bisa diserialisasi seperti yang dilakukan k6 antara setup dan VU', async () => {
    const { module, backend } = await load({}, { runMs: 1000, queueMs: 200 })
    const data = JSON.parse(JSON.stringify(module.setup()))
    module.jobs(data)
    assert.equal(backend.state.logins, 12)
  })

  it('tanpa sesi di data VU tetap bisa masuk sendiri sebagai cadangan', async () => {
    const { module, backend } = await load()
    const data = module.setup()
    const before = backend.state.logins
    module.reads({ ...data, sessions: {} })
    assert.equal(backend.state.logins, before + 1)
  })

  it('login yang dibatasi server ditunggu sesuai Retry-After lalu diulang sampai berhasil', async () => {
    const { module, backend } = await load({ SCENARIOS: 'reads', SESSIONS_PER_ACCOUNT: '2' }, { loginRateLimits: 2 })
    const data = module.setup()
    assert.equal(data.sessions.editor.length, 2)
    assert.equal(backend.state.logins, 2)
    // Dua penolakan masing-masing menunggu 2 detik pada jam buatan
    assert.ok(backend.clock.t >= 4000)
  })

  it('login yang terus dibatasi menyerah setelah tiga percobaan dengan pesan yang jelas', async () => {
    const { module } = await load({ SCENARIOS: 'reads' }, { loginRateLimits: 99 })
    assert.throws(() => module.setup(), /login editor gagal/)
  })

  it('setup diberi batas waktu yang cukup untuk menunggu pembatas login', async () => {
    const { module } = await load()
    assert.equal(module.options.setupTimeout, '5m')
  })

  // Permintaan sisa dibuat langsung di state backend palsu seolah-olah dari run k6 sebelumnya yang terganggu
  const approvals = (decisions) => [
    { group_id: 'g-writers', leader_user_id: 'u-reviewer', decision: decisions[0] },
    { group_id: 'g-readers', leader_user_id: 'u-observer', decision: decisions[1] },
  ]
  const leftover = (backend, { title, status, decisions, database = 'db-1' }) => {
    backend.state.migrations.push({ id: `mig-${title}`, database_id: database, revision: 1, title })
    backend.state.requests.push({ id: `cr-${title}`, database_id: database, migration_id: `mig-${title}`, revision: 1 + decisions.filter(Boolean).length, kind: 'apply', status, approvals: approvals(decisions) })
  }
  const LEADERS = { LEADER_USERS: 'reviewer,observer' }

  it('setup menjalankan permintaan approved milik k6 yang tertinggal sehingga database tidak lagi ditahan', async () => {
    const { module, backend } = await load({ ...LEADERS }, { runMs: 1000, queueMs: 200 })
    leftover(backend, { title: 'k6 k6_lama', status: 'approved', decisions: ['approve', 'approve'] })
    const data = module.setup()
    assert.equal(backend.state.jobs.length, 1)
    assert.equal(backend.state.jobs[0].change_request_id, 'cr-k6 k6_lama')
    assert.equal(backend.state.requests.find((request) => request.id === 'cr-k6 k6_lama').status, 'queued')
    // Database kini bebas sehingga satu iterasi job berhasil tanpa konflik
    module.jobs(data)
    assert.equal(backend.state.requests.length, 2)
  })

  it('setup melanjutkan permintaan k6 yang baru disetujui sebagian lalu menjalankannya', async () => {
    const { module, backend } = await load({ ...LEADERS }, { runMs: 1000, queueMs: 200 })
    leftover(backend, { title: 'k6 k6_sebagian', status: 'pending_review', decisions: ['approve', null] })
    module.setup()
    const request = backend.state.requests[0]
    assert.deepEqual(request.approvals.map((approval) => approval.decision), ['approve', 'approve'])
    assert.equal(backend.state.jobs.length, 1)
  })

  it('setup menolak menyentuh permintaan terbuka yang bukan milik k6 dan menjelaskan penyebabnya', async () => {
    const { module, backend } = await load({ ...LEADERS })
    leftover(backend, { title: 'Migration milik tim', status: 'approved', decisions: ['approve', 'approve'] })
    assert.throws(() => module.setup(), /ditahan permintaan cr-Migration milik tim \(approved\) yang bukan milik k6/)
    assert.equal(backend.state.jobs.length, 0)
  })

  it('setup tidak mengurus permintaan yang sudah selesai dan tidak membuat job tambahan', async () => {
    const { module, backend } = await load({ ...LEADERS })
    leftover(backend, { title: 'k6 selesai', status: 'succeeded', decisions: ['approve', 'approve'] })
    module.setup()
    assert.equal(backend.state.jobs.length, 0)
  })

  it('mode baca saja tidak menyelesaikan permintaan apa pun', async () => {
    const { module, backend } = await load({ SCENARIOS: 'reads' })
    leftover(backend, { title: 'k6 k6_lama', status: 'approved', decisions: ['approve', 'approve'] })
    module.setup()
    assert.equal(backend.state.jobs.length, 0)
  })

  it('SESSIONS_PER_ACCOUNT mengatur jumlah sesi per akun dan dibatasi antara 1 dan 5 sesuai batas login server', async () => {
    const counts = {}
    for (const value of ['2', '9', '0', 'banyak', '3.7']) {
      const { module, backend } = await load({ SESSIONS_PER_ACCOUNT: value })
      module.setup()
      counts[value] = backend.state.logins
    }
    assert.deepEqual(counts, { 2: 6, 9: 15, 0: 3, banyak: 12, '3.7': 9 })
  })

  it('VU berbeda memakai sesi berbeda dari kolam dan berputar bila VU lebih banyak daripada sesi', async () => {
    const first = await load({ SCENARIOS: 'reads', SESSIONS_PER_ACCOUNT: '3' })
    const data = first.module.setup()
    const pool = data.sessions.editor.map((saved) => saved.cookies.sid[0])
    assert.equal(new Set(pool).size, 3)
    const used = []
    for (const id of [1, 2, 3, 4, 5, 6, 7]) {
      const vu = await load({ SCENARIOS: 'reads', SESSIONS_PER_ACCOUNT: '3' }, {}, first.backend)
      vu.k6.execution.vu.idInTest = id
      const before = first.backend.state.log.length
      vu.module.reads(data)
      used.push(first.backend.state.log.slice(before).find((entry) => entry.tags?.kind === 'read').sid)
    }
    assert.deepEqual(used, [pool[0], pool[1], pool[2], pool[0], pool[1], pool[2], pool[0]])
    assert.equal(first.backend.state.logins, 3)
  })

  it('SCENARIOS=reads hanya mendaftarkan scenario baca dan setup tidak login sebagai leader', async () => {
    const { module, backend } = await load({ SCENARIOS: 'reads', LEADER_USERS: 'hantu' })
    assert.deepEqual(Object.keys(module.options.scenarios), ['reads'])
    assert.equal('job_success' in module.options.thresholds, false)
    const data = module.setup()
    assert.equal(backend.state.logins, 4)
    assert.deepEqual(Object.keys(data.sessions), ['editor'])
  })

  it('SCENARIOS=jobs hanya mendaftarkan scenario job', async () => {
    const { module } = await load({ SCENARIOS: 'jobs' })
    assert.deepEqual(Object.keys(module.options.scenarios), ['jobs'])
    assert.deepEqual(module.options.thresholds.job_success, ['rate>0.95'])
  })

  it('mode baca saja memilih database PostgreSQL yang bisa dilihat dan mode job hanya yang bisa diubah', async () => {
    const reads = await load({ SCENARIOS: 'reads', DB_COUNT: '5' })
    assert.deepEqual(reads.module.setup().databases.map((database) => database.name), ['inventaris', 'pelaporan', 'catatan'])
    const both = await load({ DB_COUNT: '5' })
    assert.deepEqual(both.module.setup().databases.map((database) => database.name), ['inventaris', 'pelaporan'])
  })

  it('mode baca saja gagal dengan pesan jelas bila tidak ada database yang cocok', async () => {
    const { module } = await load({ SCENARIOS: 'reads', DATABASES: 'tidak-ada' })
    assert.throws(() => module.setup(), /tidak ada database PostgreSQL yang bisa dilihat/)
  })

  it('kata sandi per akun dipakai bila tiap akun punya kata sandi sendiri', async () => {
    const passwords = { editor: 'sandi-e', reviewer: 'sandi-r', observer: 'sandi-o' }
    const separate = await load({ K6_PASSWORD: '', K6_PASSWORD_EDITOR: 'sandi-e', K6_PASSWORD_REVIEWER: 'sandi-r', K6_PASSWORD_OBSERVER: 'sandi-o' }, { passwords })
    assert.equal(Object.keys(separate.module.setup().sessions).length, 3)
    const shared = await load({ K6_PASSWORD: 'sandi-e' }, { passwords })
    assert.throws(() => shared.module.setup(), /login leader reviewer gagal/)
  })

  it('READ_WORKFLOW=false membuat scenario baca tidak pernah menyentuh endpoint workflow', async () => {
    const { module, backend } = await load({ SCENARIOS: 'reads', READ_WORKFLOW: 'false' })
    const data = module.setup()
    const before = backend.state.log.length
    for (let index = 0; index < 40; index++) module.reads(data)
    const paths = backend.state.log.slice(before).filter((entry) => entry.tags?.kind === 'read').map((entry) => entry.path)
    assert.equal(paths.length, 40)
    for (const path of paths) assert.doesNotMatch(path, /position|executions|migrations|change-requests|jobs/)
  })
})
