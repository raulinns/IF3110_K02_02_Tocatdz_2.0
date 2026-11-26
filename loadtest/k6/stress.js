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

export const options = {
  scenarios: {
    reads: { executor: 'ramping-vus', exec: 'reads', startVUs: 1, stages: scenarioStages(config, config.readVus), gracefulRampDown: '30s' },
    jobs: {
      executor: 'ramping-vus',
      exec: 'jobs',
      startVUs: 1,
      stages: scenarioStages(config, config.jobVus),
      gracefulRampDown: `${config.jobTimeoutSeconds}s`,
      gracefulStop: `${config.jobTimeoutSeconds}s`,
    },
  },
  thresholds: buildThresholds(config, stageLabels(config.multipliers)),
  summaryTrendStats: ['avg', 'min', 'med', 'p(90)', 'p(95)', 'p(99)', 'max'],
}

// Tahap dihitung dari waktu sejak scenario dimulai supaya setiap permintaan dan metrik membawa tag tahapnya
const stage = () => stageAt(config, (Date.now() - exec.scenario.startTime) / 1000)

// Modul ini berjalan terpisah di setiap VU sehingga sesi yang disimpan di sini tidak dibagi antar VU
const sessions = {}

function sessionFor(username) {
  if (!sessions[username]) {
    const session = client.newSession(username, config.password)
    const res = client.login(session)
    if (res.status !== 200) fail(`login ${username} gagal dengan status ${res.status}`)
    sessions[username] = session
  }
  return sessions[username]
}

// Database target dipilih dari yang bisa dipakai editor, dan SAME_DB memaksa semua VU memakai database pertama
function pickDatabases(items) {
  const usable = items.filter((database) => database.access === 'view_edit' && database.capabilities?.workflow)
  const named = config.databases.length > 0 ? usable.filter((database) => config.databases.includes(database.name) || config.databases.includes(database.id)) : usable
  return named.slice(0, config.sameDb ? 1 : config.dbCount)
}

export function setup() {
  const editor = client.newSession(config.editor, config.password)
  if (client.login(editor).status !== 200) fail(`login ${config.editor} gagal, periksa EDITOR_USER dan K6_PASSWORD`)
  for (const name of config.leaders) {
    const leader = client.newSession(name, config.password)
    if (client.login(leader).status !== 200) fail(`login leader ${name} gagal, periksa LEADER_USERS dan K6_PASSWORD`)
  }
  const list = client.request(editor, 'GET', '/databases?page=1&limit=200', { name: 'GET /databases' })
  if (list.status !== 200) fail(`daftar database gagal dengan status ${list.status}`)
  const databases = pickDatabases(client.parse(list).items)
  if (databases.length === 0) fail('tidak ada database PostgreSQL dengan akses ubah untuk editor, periksa seed dan DATABASES')
  for (const database of databases) {
    const schema = client.request(editor, 'GET', `/databases/${database.id}/schema`, { name: 'GET /databases/{id}/schema' })
    const first = schema.status === 200 ? client.parse(schema).schemas?.find((item) => item.tables.length > 0) : undefined
    database.table = first ? { schema: first.name, name: first.tables[0].name } : null
  }
  return { runId: Date.now().toString(36), databases: databases.map((database) => ({ id: database.id, name: database.name, table: database.table })) }
}

export function reads(data) {
  const database = data.databases[Math.floor(Math.random() * data.databases.length)]
  readOnce({ client, session: sessionFor(config.editor), database, stage, random: Math.random, check })
  sleep(0.2 + Math.random() * 0.6)
}

export function jobs(data) {
  const database = config.sameDb ? data.databases[0] : data.databases[(exec.vu.idInTest - 1) % data.databases.length]
  const plan = {
    editor: sessionFor(config.editor),
    leaders: Object.fromEntries(config.leaders.map((name) => [name, sessionFor(name)])),
    database,
    stage,
  }
  const table = `k6_${data.runId}_${exec.vu.idInTest}_${exec.scenario.iterationInTest}`
  runJob({ client, config, metrics, sleep, now: Date.now, random: Math.random }, plan, table)
}
