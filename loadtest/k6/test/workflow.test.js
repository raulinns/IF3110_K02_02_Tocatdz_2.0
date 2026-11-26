import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { createClient } from '../lib/client.js'
import { loadConfig } from '../lib/config.js'
import { definitionFor, runJob } from '../lib/workflow.js'
import { createBackend, createMetrics } from './fake-backend.js'

function setup(options = {}, env = {}) {
  const backend = createBackend(options)
  const client = createClient({ http: backend.http, baseUrl: 'http://localhost:8080', origin: 'http://localhost:8080' })
  const config = loadConfig({ POLL_SECONDS: '1', JOB_TIMEOUT_SECONDS: '30', SUBMIT_RETRIES: '5', ...env })
  const sessions = {}
  for (const name of ['editor', 'reviewer', 'observer']) {
    sessions[name] = client.newSession(name, 'pw')
    client.login(sessions[name])
  }
  const metrics = createMetrics()
  const deps = { client, config, metrics, sleep: backend.sleep, now: backend.now, random: () => 0.5 }
  const plan = { editor: sessions.editor, leaders: { reviewer: sessions.reviewer, observer: sessions.observer }, database: backend.databases[0], stage: () => 'tahap1-x1' }
  return { backend, deps, plan, metrics, sessions }
}

const calls = (backend, method, fragment) => backend.state.log.filter((entry) => entry.method === method && entry.path.includes(fragment))

describe('definisi migration uji', () => {
  it('membuat tabel unik dengan SQL up dan down yang saling membatalkan dalam mode normal', () => {
    const definition = definitionFor('k6_abc_1_0')
    assert.match(definition.up_sql, /CREATE TABLE IF NOT EXISTS public\.k6_abc_1_0/)
    assert.match(definition.down_sql, /DROP TABLE IF EXISTS public\.k6_abc_1_0/)
    assert.equal(definition.mode, 'normal')
    assert.equal(definition.batch, null)
    assert.equal(definition.irreversible, false)
  })
})

describe('alur job', () => {
  it('jalur sukses: buat, ajukan, dua leader menyetujui, jalankan, lalu job selesai', () => {
    const { backend, deps, plan, metrics } = setup()
    assert.equal(runJob(deps, plan, 'k6_a_1_0'), 'succeeded')
    assert.equal(backend.state.migrations.length, 1)
    assert.equal(backend.state.requests[0].status, 'queued')
    assert.equal(calls(backend, 'POST', '/approvals').length, 2)
    assert.equal(calls(backend, 'POST', '/execute').length, 1)
    assert.equal(metrics.jobsCompleted.values.length, 1)
    assert.equal(metrics.jobsFailed.values.length, 0)
    assert.deepEqual(metrics.jobSuccess.values.map((entry) => entry.value), [true])
  })

  it('keberhasilan hanya dihitung setelah polling menunjukkan hasil akhir dan bukan dari respons 202', () => {
    const { backend, deps, plan, metrics } = setup({ queueMs: 2000, runMs: 5000 })
    const seen = []
    const sleep = (seconds) => {
      seen.push(metrics.jobsCompleted.values.length)
      backend.sleep(seconds)
    }
    runJob({ ...deps, sleep }, plan, 'k6_a_1_0')
    assert.ok(seen.length >= 5, 'polling berjalan beberapa kali')
    assert.ok(seen.every((count) => count === 0), 'tidak ada job selesai sebelum status akhir')
    assert.equal(metrics.jobsCompleted.values.length, 1)
  })

  it('mencatat waktu tunggu antrean, durasi job, dan durasi ujung ke ujung dari jam server', () => {
    const { deps, plan, metrics } = setup({ queueMs: 2000, runMs: 5000 })
    runJob(deps, plan, 'k6_a_1_0')
    assert.equal(metrics.queueWait.values[0].value, 2000)
    assert.equal(metrics.jobDuration.values[0].value, 5000)
    assert.ok(metrics.jobEndToEnd.values[0].value >= 7000)
    assert.deepEqual(metrics.queueWait.values[0].tags, { stage: 'tahap1-x1' })
  })

  it('persetujuan memakai sesi leader yang tepat untuk tiap group dengan revision terbaru', () => {
    const { backend, deps, plan } = setup()
    runJob(deps, plan, 'k6_a_1_0')
    const bodies = calls(backend, 'POST', '/approvals')
    assert.equal(bodies.length, 2)
    assert.deepEqual(backend.state.requests[0].approvals.map((approval) => approval.decision), ['approve', 'approve'])
  })

  it('semua permintaan membawa tag nama tanpa ID dan tag jenis', () => {
    const { backend, deps, plan } = setup()
    runJob(deps, plan, 'k6_a_1_0')
    const flow = backend.state.log.filter((entry) => entry.tags?.kind)
    assert.ok(flow.length > 0)
    for (const entry of flow) {
      assert.match(entry.tags.name, /^(GET|POST) \//)
      assert.doesNotMatch(entry.tags.name, /db-\d|cr-\d|job-\d|mig-\d/)
      assert.equal(entry.tags.stage, 'tahap1-x1')
    }
    assert.deepEqual([...new Set(flow.map((entry) => entry.tags.kind))].sort(), ['poll', 'write'])
  })

  it('database yang masih ditahan permintaan lain membuat pengajuan menunggu dan konflik dihitung', () => {
    const { deps, plan, metrics } = setup({ conflicts: 3 })
    assert.equal(runJob(deps, plan, 'k6_a_1_0'), 'succeeded')
    assert.equal(metrics.submitConflicts.values.length, 3)
    assert.equal(metrics.submitWait.values.length, 1)
    assert.ok(metrics.submitWait.values[0].value >= 3000, 'tiga kali tunggu sekitar dua detik')
  })

  it('draft hanya dibuat sekali walau pengajuan diulang', () => {
    const { backend, deps, plan } = setup({ conflicts: 3 })
    runJob(deps, plan, 'k6_a_1_0')
    assert.equal(calls(backend, 'POST', '/migrations').length, 1)
    assert.equal(calls(backend, 'POST', '/change-requests').filter((entry) => !entry.path.includes('/approvals') && !entry.path.includes('/execute')).length, 4)
  })

  it('konflik yang tidak kunjung selesai berhenti setelah batas percobaan dan dicatat sebagai galat', () => {
    const { deps, plan, metrics } = setup({ conflicts: 99 }, { SUBMIT_RETRIES: '2' })
    assert.equal(runJob(deps, plan, 'k6_a_1_0'), 'conflict_exhausted')
    assert.equal(metrics.submitConflicts.values.length, 3)
    assert.equal(metrics.flowErrors.values[0].tags.reason, 'conflict_exhausted')
    assert.deepEqual(metrics.jobSuccess.values.map((entry) => entry.value), [false])
  })

  it('job yang gagal tidak dihitung selesai dan masuk hitungan gagal', () => {
    const { deps, plan, metrics } = setup({ failJobs: true })
    assert.equal(runJob(deps, plan, 'k6_a_1_0'), 'failed')
    assert.equal(metrics.jobsCompleted.values.length, 0)
    assert.equal(metrics.jobsFailed.values.length, 1)
    assert.deepEqual(metrics.jobSuccess.values.map((entry) => entry.value), [false])
  })

  it('job yang tidak kunjung selesai berhenti saat batas waktu dan dihitung timeout', () => {
    const { backend, deps, plan, metrics } = setup({ stuckJobs: true }, { JOB_TIMEOUT_SECONDS: '10' })
    assert.equal(runJob(deps, plan, 'k6_a_1_0'), 'timeout')
    assert.equal(metrics.jobsTimedOut.values.length, 1)
    assert.equal(metrics.jobsCompleted.values.length, 0)
    assert.ok(backend.clock.t >= 10_000 && backend.clock.t < 15_000)
  })

  it('respons 429 ditunggu sesuai Retry-After lalu permintaan diulang', () => {
    const { backend, deps, plan, metrics, sessions } = setup()
    let limited = 2
    const original = backend.http.request
    backend.http.request = (method, url, body, params) => {
      if (limited > 0 && url.endsWith('/migrations')) {
        limited -= 1
        return { status: 429, headers: { 'Retry-After': '3' }, body: '', json: () => ({}) }
      }
      return original(method, url, body, params)
    }
    const before = backend.clock.t
    assert.equal(runJob(deps, plan, 'k6_a_1_0'), 'succeeded')
    assert.equal(metrics.rateLimited.values.length, 2)
    assert.ok(backend.clock.t - before >= 6000)
    assert.ok(sessions.editor)
  })

  it('leader yang tidak dikenal menggagalkan persetujuan dengan alasan yang jelas', () => {
    const { deps, plan, metrics } = setup()
    plan.leaders = { reviewer: plan.leaders.reviewer }
    assert.equal(runJob(deps, plan, 'k6_a_1_0'), 'leader_unavailable')
    assert.equal(metrics.flowErrors.values[0].tags.reason, 'leader_unavailable')
  })

  it('draft yang ditolak server menghentikan alur tanpa mengajukan apa pun', () => {
    const { backend, deps, plan } = setup()
    const original = backend.http.request
    backend.http.request = (method, url, body, params) => (url.endsWith('/migrations') && method === 'POST' ? { status: 422, headers: {}, body: '{}', json: () => ({}) } : original(method, url, body, params))
    assert.equal(runJob(deps, plan, 'k6_a_1_0'), 'create_failed')
    assert.equal(calls(backend, 'POST', '/change-requests').length, 0)
  })

  it('dua iterasi berurutan pada database yang sama berhasil karena permintaan pertama sudah selesai', () => {
    const { deps, plan, metrics } = setup()
    assert.equal(runJob(deps, plan, 'k6_a_1_0'), 'succeeded')
    assert.equal(runJob(deps, plan, 'k6_a_1_1'), 'succeeded')
    assert.equal(metrics.jobsCompleted.values.length, 2)
    assert.equal(metrics.submitConflicts.values.length, 0)
  })
})
