import { check, fail, sleep } from 'k6'
import exec from 'k6/execution'
import http from 'k6/http'
import { Counter, Rate, Trend } from 'k6/metrics'
import { createClient } from './lib/client.js'
import { loadConfig } from './lib/config.js'
import { readOnce } from './lib/reads.js'
import { scenarioStages, stageAt, stageLabels } from './lib/stages.js'
import { buildThresholds } from './lib/thresholds.js'
import { runJob } from './lib/workflow.js'

const config = loadConfig(__ENV)
const withReads = config.scenarios.includes('reads')
const withJobs = config.scenarios.includes('jobs')

// Hanya 2xx yang dianggap berhasil, dan konflik yang memang diharapkan menyatakan statusnya sendiri per permintaan
http.setResponseCallback(http.expectedStatuses({ min: 200, max: 299 }))

const client = createClient({ http, baseUrl: config.baseUrl, origin: config.origin })

const metrics = {
  jobsCompleted: new Counter('jobs_completed'),
  jobsFailed: new Counter('jobs_failed'),
  jobsTimedOut: new Counter('jobs_timed_out'),
  submitConflicts: new Counter('submit_conflicts'),
  rateLimited: new Counter('rate_limited'),
  flowErrors: new Counter('flow_errors'),
  jobSuccess: new Rate('job_success'),
  queueWait: new Trend('job_queue_wait_ms', true),
  jobDuration: new Trend('job_duration_ms', true),
  jobEndToEnd: new Trend('job_e2e_ms', true),
  submitWait: new Trend('submit_wait_ms', true),
}

// Scenario yang tidak dipilih tidak didaftarkan, sehingga baca saja bisa dijalankan sebelum alur job tersedia di server
const scenarios = {}
if (withReads) scenarios.reads = { executor: 'ramping-vus', exec: 'reads', startVUs: 1, stages: scenarioStages(config, config.readVus), gracefulRampDown: '30s' }
if (withJobs) {
  scenarios.jobs = {
    executor: 'ramping-vus',
    exec: 'jobs',
    startVUs: 1,
    stages: scenarioStages(config, config.jobVus),
    gracefulRampDown: `${config.jobTimeoutSeconds}s`,
    gracefulStop: `${config.jobTimeoutSeconds}s`,
  }
}

export const options = {
  scenarios,
  thresholds: buildThresholds(config, stageLabels(config.multipliers)),
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
}

// Tahap dihitung dari waktu sejak scenario dimulai supaya setiap permintaan dan metrik membawa tag tahapnya
const stage = () => stageAt(config, (Date.now() - exec.scenario.startTime) / 1000)

// Modul ini berjalan terpisah di setiap VU sehingga sesi yang disimpan di sini tidak dibagi antar VU
const sessions = {}

// Sesi diambil dari kolam hasil login di setup() karena server membatasi login per akun, dan tiap VU tetap memakai satu sesi yang sama sepanjang run
// Login langsung hanya cadangan bila kolam tidak ada
function sessionFor(username, data) {
  if (!sessions[username]) {
    const pool = data?.sessions?.[username]
    if (pool?.length) {
      sessions[username] = client.adoptSession(pool[(exec.vu.idInTest - 1) % pool.length], config.passwordFor(username))
    } else {
      const session = client.newSession(username, config.passwordFor(username))
      const res = client.login(session)
      if (res.status !== 200) fail(`login ${username} gagal dengan status ${res.status}`)
      sessions[username] = session
    }
  }
  return sessions[username]
}

// Database target dipilih dari yang bisa dipakai editor, dan SAME_DB memaksa semua VU memakai database pertama
// Pembacaan saja cukup dengan database PostgreSQL yang bisa dilihat, sedangkan alur job butuh akses ubah dan workflow
function pickDatabases(items) {
  const usable = items.filter((database) => (withJobs ? database.access === 'view_edit' && database.capabilities?.workflow : database.engine === 'postgresql'))
  const named = config.databases.length > 0 ? usable.filter((database) => config.databases.includes(database.name) || config.databases.includes(database.id)) : usable
  return named.slice(0, config.sameDb ? 1 : config.dbCount)
}

// Satu akun masuk beberapa kali sekaligus supaya VU tidak berebut satu sesi, dan jumlahnya dibatasi di bawah batas login server per menit
function loginPool(username, failure) {
  const live = []
  for (let index = 0; index < config.sessionsPerAccount; index++) {
    const session = client.newSession(username, config.passwordFor(username))
    if (client.login(session).status !== 200) fail(failure)
    live.push(session)
  }
  return { first: live[0], saved: live.map((session) => client.exportSession(session)) }
}

export function setup() {
  const saved = {}
  const editorPool = loginPool(config.editor, `login ${config.editor} gagal, periksa EDITOR_USER dan kata sandinya (K6_PASSWORD atau K6_PASSWORD_<USERNAME>)`)
  const editor = editorPool.first
  saved[config.editor] = editorPool.saved
  if (withJobs) {
    for (const name of config.leaders) saved[name] = loginPool(name, `login leader ${name} gagal, periksa LEADER_USERS dan kata sandinya (K6_PASSWORD atau K6_PASSWORD_<USERNAME>)`).saved
  }
  const list = client.request(editor, 'GET', '/databases?page=1&limit=200', { name: 'GET /databases' })
  if (list.status !== 200) fail(`daftar database gagal dengan status ${list.status}`)
  const databases = pickDatabases(client.parse(list).items)
  if (databases.length === 0) fail(withJobs ? 'tidak ada database PostgreSQL dengan akses ubah untuk editor, periksa seed dan DATABASES' : 'tidak ada database PostgreSQL yang bisa dilihat editor, periksa seed dan DATABASES')
  for (const database of databases) {
    const schema = client.request(editor, 'GET', `/databases/${database.id}/schema`, { name: 'GET /databases/{id}/schema' })
    const first = schema.status === 200 ? client.parse(schema).schemas?.find((item) => item.tables.length > 0) : undefined
    database.table = first ? { schema: first.name, name: first.tables[0].name } : null
  }
  return { runId: Date.now().toString(36), sessions: saved, databases: databases.map((database) => ({ id: database.id, name: database.name, table: database.table })) }
}

export function reads(data) {
  const database = data.databases[Math.floor(Math.random() * data.databases.length)]
  readOnce({ client, session: sessionFor(config.editor, data), database, stage, random: Math.random, check, workflow: config.readWorkflow })
  sleep(0.2 + Math.random() * 0.6)
}

export function jobs(data) {
  const database = config.sameDb ? data.databases[0] : data.databases[(exec.vu.idInTest - 1) % data.databases.length]
  const plan = {
    editor: sessionFor(config.editor, data),
    leaders: Object.fromEntries(config.leaders.map((name) => [name, sessionFor(name, data)])),
    database,
    stage,
  }
  const table = `k6_${data.runId}_${exec.vu.idInTest}_${exec.scenario.iterationInTest}`
  runJob({ client, config, metrics, sleep, now: Date.now, random: Math.random }, plan, table)
}
