import type { Schemas } from '../api/types.ts'
import { appendExecution, bumpPosition } from './position.ts'
import { getState, now, type JobPlan, type MockChangeRequest, type MockJob, type MockMigration, type Scenario } from './state.ts'

const maxAttempts = 3
const reconcileTicks = 2

const failures = {
  sql: { code: 'sql_error', message: 'Statement SQL gagal dan transaksi dibatalkan, tidak ada perubahan dari percobaan ini yang tersimpan.', sqlstate: '42P01', retryable: false },
  connection: { code: 'connection_error', message: 'Koneksi ke database target terputus, percobaan akan diulang.', sqlstate: '08006', retryable: true },
  interrupted: { code: 'sql_error', message: 'Batch berikutnya gagal, batch yang sudah di-commit tetap tersimpan.', sqlstate: '23505', retryable: false },
  compensation: { code: 'sql_error', message: 'Pembatalan batch berhenti di tengah, batch yang sudah dibatalkan tetap tersimpan.', sqlstate: '23505', retryable: false },
  uncertain: { code: 'commit_uncertain', message: 'Hasil commit belum dapat dipastikan, menunggu rekonsiliasi.', sqlstate: null, retryable: false },
} satisfies Record<string, Schemas['JobFailure']>

// Komentar seperti -- mock:fail pada SQL memilih hasil akhir job, tanpa komentar job selalu berhasil
export function scenarioOf(sql: string): Scenario {
  const match = /--\s*mock:(fail|retry|partial|uncertain)\b/i.exec(sql)
  return match ? (match[1].toLowerCase() as Scenario) : 'ok'
}

// Jumlah baris tabel target menentukan banyak batch, dan tabel yang tidak dikenal berarti total tidak diketahui
function planBatches(migration: MockMigration, scenario: Scenario): { batches: number[]; totalKnown: boolean } {
  const config = migration.batch
  if (migration.mode !== 'batch' || !config) return { batches: [], totalKnown: true }
  const table = getState().tables.find((candidate) => candidate.database_id === migration.database_id && candidate.schema === config.schema && candidate.name === config.table)
  const rows = table ? table.rows.length : config.batch_size * 3
  const batches: number[] = []
  for (let remaining = rows; remaining > 0; remaining -= config.batch_size) batches.push(Math.min(config.batch_size, remaining))
  while ((scenario === 'partial' || scenario === 'uncertain') && batches.length < 3) batches.push(config.batch_size)
  return { batches, totalKnown: table !== undefined }
}

function emptyProgress(): Schemas['Progress'] {
  return { batches_committed: 0, rows_committed: 0, batches_reverted: 0, rows_reverted: 0, total_batches: null }
}

// Resume melanjutkan batch yang tersisa dan revert membatalkan batch yang sudah di-commit dalam urutan terbalik
function planFor(request: MockChangeRequest, migration: MockMigration, source: MockJob | undefined): { plan: JobPlan; progress: Schemas['Progress']; executionId: string } {
  const base = { done: 0, resume: false, uncertainAt: null }
  if (source && (request.kind === 'resume' || request.kind === 'revert_batches')) {
    const compensate = request.kind === 'revert_batches' || source.status === 'partially_reverted'
    const direction: Schemas['Direction'] = compensate ? 'down' : 'up'
    const sql = (compensate ? migration.down_sql : migration.up_sql) ?? ''
    const range = compensate ? source.plan.batches.slice(0, source.progress.batches_committed - source.progress.batches_reverted) : source.plan.batches.slice(source.progress.batches_committed)
    const scenario = request.kind === 'revert_batches' && scenarioOf(sql) === 'partial' ? 'partial' : 'ok'
    return {
      plan: { ...base, direction, sql, scenario, batches: range, totalKnown: source.plan.totalKnown, compensate, resume: request.kind === 'resume' },
      progress: { ...source.progress },
      executionId: request.kind === 'resume' ? source.execution_id : crypto.randomUUID(),
    }
  }
  const direction: Schemas['Direction'] = request.kind === 'rollback' ? 'down' : 'up'
  const sql = (direction === 'down' ? migration.down_sql : migration.up_sql) ?? ''
  const scenario = scenarioOf(sql)
  const { batches, totalKnown } = planBatches(migration, scenario)
  return { plan: { ...base, direction, sql, scenario, batches, totalKnown, compensate: false }, progress: { ...emptyProgress(), total_batches: totalKnown && batches.length > 0 ? batches.length : null }, executionId: crypto.randomUUID() }
}

// Job masuk antrean dalam kondisi queued dan baru bergerak ketika dipolling
export function createJob(request: MockChangeRequest, migration: MockMigration): MockJob {
  const state = getState()
  const source = state.jobs.find((candidate) => candidate.id === request.source_job_id)
  const { plan, progress, executionId } = planFor(request, migration, source)
  const queuedAt = now()
  const job: MockJob = {
    id: crypto.randomUUID(),
    database_id: request.database_id,
    change_request_id: request.id,
    execution_id: executionId,
    migration_id: migration.id,
    kind: request.kind,
    mode: migration.mode,
    status: 'queued',
    attempt: 0,
    max_attempts: maxAttempts,
    run_after: queuedAt,
    queued_at: queuedAt,
    started_at: null,
    finished_at: null,
    partial: false,
    progress,
    error: null,
    attempts: [],
    plan,
    ticks: 0,
    resolved: false,
  }
  state.jobs.push(job)
  return job
}

function setRequestStatus(request: MockChangeRequest, status: MockChangeRequest['status']) {
  request.status = status
  request.revision += 1
  request.updated_at = now()
}

function closeAttempt(job: MockJob, status: Schemas['JobStatus'], error: Schemas['JobFailure'] | null, finished: boolean) {
  const attempt = job.attempts.at(-1)
  if (!attempt) return
  attempt.status = status
  attempt.error = error
  if (finished) attempt.finished_at = now()
}

function finish(job: MockJob, request: MockChangeRequest, status: 'failed' | 'partial' | 'partially_reverted', error: Schemas['JobFailure'], partial: boolean) {
  job.status = status
  job.error = error
  job.partial = partial
  job.finished_at = now()
  closeAttempt(job, status, error, true)
  setRequestStatus(request, status)
  if (partial) bumpPosition(job.database_id)
}

function succeed(job: MockJob, request: MockChangeRequest) {
  const state = getState()
  const { plan } = job
  if (!plan.compensate) {
    job.progress.batches_committed += plan.batches.length - plan.done
    job.progress.rows_committed += plan.batches.slice(plan.done).reduce((sum, rows) => sum + rows, 0)
    if (plan.batches.length > 0 && plan.totalKnown) job.progress.total_batches = job.progress.batches_committed
  } else {
    const rest = plan.batches.slice(0, plan.batches.length - plan.done)
    job.progress.batches_reverted += rest.length
    job.progress.rows_reverted += rest.reduce((sum, rows) => sum + rows, 0)
  }
  plan.done = plan.batches.length
  job.status = 'succeeded'
  job.error = null
  job.partial = false
  job.finished_at = now()
  closeAttempt(job, 'succeeded', null, true)
  setRequestStatus(request, 'succeeded')
  const migration = state.migrations.find((candidate) => candidate.id === job.migration_id)
  if (migration) appendExecution(job, request, migration)
  if (plan.resume || plan.compensate) {
    resolveChain(request)
    bumpPosition(job.database_id)
  }
}

// Pemulihan yang selesai menutup seluruh rantai job sumbernya, misalnya partial lalu partially_reverted
function resolveChain(request: MockChangeRequest) {
  const state = getState()
  let source = state.jobs.find((candidate) => candidate.id === request.source_job_id)
  while (source) {
    source.resolved = true
    const origin: MockChangeRequest | undefined = state.changeRequests.find((candidate) => candidate.id === source?.change_request_id)
    source = state.jobs.find((candidate) => candidate.id === origin?.source_job_id)
  }
}

function commitBatch(job: MockJob) {
  const { plan, progress } = job
  if (plan.compensate) {
    progress.batches_reverted += 1
    progress.rows_reverted += plan.batches[plan.batches.length - 1 - plan.done]
  } else {
    progress.batches_committed += 1
    progress.rows_committed += plan.batches[plan.done]
  }
  plan.done += 1
}

function start(job: MockJob, request: MockChangeRequest) {
  job.status = 'running'
  job.attempt += 1
  job.started_at ??= now()
  job.attempts.push({ attempt: job.attempt, status: 'running', started_at: now(), finished_at: null, error: null })
  setRequestStatus(request, 'running')
}

function retryLater(job: MockJob, request: MockChangeRequest) {
  closeAttempt(job, 'failed', failures.connection, true)
  job.status = 'queued'
  job.run_after = now()
  setRequestStatus(request, 'queued')
}

function markUncertain(job: MockJob, request: MockChangeRequest) {
  job.status = 'uncertain'
  job.error = failures.uncertain
  job.partial = job.progress.batches_committed > 0
  job.plan.uncertainAt = job.ticks
  closeAttempt(job, 'uncertain', failures.uncertain, false)
  setRequestStatus(request, 'uncertain')
  bumpPosition(job.database_id)
}

// Satu tick menjalankan satu batch atau menyelesaikan job normal, dan scenario menentukan di mana job berhenti
function step(job: MockJob, request: MockChangeRequest) {
  const { plan } = job
  const total = plan.batches.length
  if (plan.scenario === 'retry' && job.attempt === 1) return retryLater(job, request)
  if (plan.scenario === 'fail' && plan.done === 0) return finish(job, request, 'failed', failures.sql, false)
  if (plan.scenario === 'uncertain' && plan.done === (total === 0 ? 0 : 1)) return markUncertain(job, request)
  if (plan.scenario === 'partial' && total > 0 && plan.done === Math.min(2, total - 1)) {
    return finish(job, request, plan.compensate ? 'partially_reverted' : 'partial', plan.compensate ? failures.compensation : failures.interrupted, true)
  }
  if (total === 0) return succeed(job, request)
  commitBatch(job)
  if (plan.done === total) succeed(job, request)
}

// Job yang belum pasti direkonsiliasi setelah beberapa kali dicek, hasilnya sukses karena receipt ditemukan
function reconcile(job: MockJob, request: MockChangeRequest) {
  if (job.plan.uncertainAt === null || job.ticks - job.plan.uncertainAt < reconcileTicks) return
  job.plan.scenario = 'ok'
  succeed(job, request)
}

// Setiap pembacaan status job memajukan simulasi satu langkah setelah snapshot dikirim
export function tickJob(job: MockJob) {
  job.ticks += 1
  const request = getState().changeRequests.find((candidate) => candidate.id === job.change_request_id)
  if (!request) return
  if (job.status === 'queued') start(job, request)
  else if (job.status === 'running') step(job, request)
  else if (job.status === 'uncertain') reconcile(job, request)
}
