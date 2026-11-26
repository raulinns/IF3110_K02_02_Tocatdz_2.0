import { parse } from './client.js'
import { durationMs, isFinal, queueWaitMs } from './jobs.js'

// Tabel uji dibuat dengan nama unik per iterasi supaya beberapa VU dan beberapa run tidak saling bertabrakan
export const definitionFor = (table) => ({
  title: `k6 ${table}`,
  up_sql: `CREATE TABLE IF NOT EXISTS public.${table} (id integer);`,
  down_sql: `DROP TABLE IF EXISTS public.${table};`,
  irreversible: false,
  irreversible_reason: '',
  mode: 'normal',
  batch: null,
})

const retryAfter = (res) => Number(res.headers?.['Retry-After'] ?? res.headers?.['retry-after']) || 1
const maxRateLimitRetries = 5

// Setiap permintaan diberi tag tahap saat itu, dan respons 429 ditunggu sesuai Retry-After lalu dicoba lagi
function call(deps, plan, session, method, path, options = {}) {
  for (let attempt = 0; ; attempt++) {
    const stage = plan.stage()
    const res = deps.client.request(session, method, path, { ...options, tags: { ...options.tags, stage } })
    if (res.status !== 429 || attempt >= maxRateLimitRetries) return res
    deps.metrics.rateLimited.add(1, { stage })
    deps.sleep(retryAfter(res))
  }
}

// Draft dibuat sekali lalu pengajuan diulang selama database masih ditahan permintaan lain, dan menunggunya adalah bagian dari beban
function submit(deps, plan, table) {
  const { config, metrics } = deps
  const created = call(deps, plan, plan.editor, 'POST', `/databases/${plan.database.id}/migrations`, { body: definitionFor(table), name: 'POST /databases/{id}/migrations', tags: { kind: 'write' } })
  if (created.status !== 201) return { error: 'create_failed' }
  const migration = parse(created)
  const waitStarted = deps.now()
  for (let retry = 0; retry <= config.submitRetries; retry++) {
    const res = call(deps, plan, plan.editor, 'POST', `/databases/${plan.database.id}/change-requests`, {
      body: { migration_id: migration.id, migration_revision: migration.revision, kind: 'apply' },
      name: 'POST /databases/{id}/change-requests',
      tags: { kind: 'write' },
      expected: [201, 409],
    })
    if (res.status === 201) {
      metrics.submitWait.add(deps.now() - waitStarted, { stage: plan.stage() })
      return { request: parse(res) }
    }
    if (res.status !== 409) return { error: 'submit_failed' }
    metrics.submitConflicts.add(1, { stage: plan.stage() })
    deps.sleep(1 + deps.random() * 2)
  }
  return { error: 'conflict_exhausted' }
}

// Setiap leader yang relevan memutuskan dengan sesinya sendiri dan revision terbaru dari respons sebelumnya
export function approve(deps, plan, request) {
  let current = request
  for (const approval of request.approvals) {
    // Keputusan yang sudah tercatat dilewati supaya permintaan yang setengah disetujui bisa dilanjutkan
    if (approval.decision) continue
    const leader = Object.values(plan.leaders).find((session) => session.user?.id === approval.leader_user_id)
    if (!leader) return { error: 'leader_unavailable' }
    const res = call(deps, plan, leader, 'POST', `/databases/${plan.database.id}/change-requests/${request.id}/approvals`, {
      body: { revision: current.revision, group_id: approval.group_id, decision: 'approve', note: 'beban k6' },
      name: 'POST /databases/{id}/change-requests/{id}/approvals',
      tags: { kind: 'write' },
      expected: [200, 409],
    })
    if (res.status !== 200) return { error: 'approval_failed' }
    current = parse(res)
  }
  return current.status === 'approved' ? { request: current } : { error: 'approval_failed' }
}

export function execute(deps, plan, request) {
  const res = call(deps, plan, plan.editor, 'POST', `/databases/${plan.database.id}/change-requests/${request.id}/execute`, {
    body: { revision: request.revision },
    name: 'POST /databases/{id}/change-requests/{id}/execute',
    tags: { kind: 'write' },
    expected: [202, 409],
  })
  if (res.status !== 202) return { error: 'execute_failed' }
  // Respons 202 hanya berarti job masuk antrean sehingga keberhasilan baru dihitung setelah polling menunjukkan hasil akhir
  return { jobId: parse(res).job_id, acceptedAt: deps.now() }
}

function poll(deps, plan, accepted) {
  const { config, metrics } = deps
  const deadline = accepted.acceptedAt + config.jobTimeoutSeconds * 1000
  let job = null
  for (;;) {
    const res = call(deps, plan, plan.editor, 'GET', `/jobs/${accepted.jobId}`, { name: 'GET /jobs/{id}', tags: { kind: 'poll' } })
    if (res.status === 200) {
      job = parse(res)
      if (isFinal(job.status)) break
    }
    if (deps.now() >= deadline) {
      metrics.jobsTimedOut.add(1, { stage: plan.stage() })
      metrics.jobSuccess.add(false, { stage: plan.stage() })
      return 'timeout'
    }
    deps.sleep(config.pollSeconds)
  }
  const tags = { stage: plan.stage() }
  const wait = queueWaitMs(job)
  const ran = durationMs(job)
  if (wait !== null) metrics.queueWait.add(wait, tags)
  if (ran !== null) metrics.jobDuration.add(ran, tags)
  metrics.jobEndToEnd.add(deps.now() - accepted.acceptedAt, tags)
  const succeeded = job.status === 'succeeded'
  if (succeeded) metrics.jobsCompleted.add(1, tags)
  else metrics.jobsFailed.add(1, tags)
  metrics.jobSuccess.add(succeeded, tags)
  return job.status
}

// Satu iterasi lengkap: buat, ajukan, setujui oleh leader, jalankan, lalu pantau sampai selesai
export function runJob(deps, plan, table) {
  const submitted = submit(deps, plan, table)
  if (submitted.error) return fail(deps, plan, submitted.error)
  const approved = approve(deps, plan, submitted.request)
  if (approved.error) return fail(deps, plan, approved.error)
  const accepted = execute(deps, plan, approved.request)
  if (accepted.error) return fail(deps, plan, accepted.error)
  return poll(deps, plan, accepted)
}

function fail(deps, plan, reason) {
  deps.metrics.flowErrors.add(1, { stage: plan.stage(), reason })
  deps.metrics.jobSuccess.add(false, { stage: plan.stage() })
  return reason
}
