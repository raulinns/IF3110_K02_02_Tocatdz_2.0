import { afterEach, describe, expect, it } from 'vitest'
import { definitionChecksum, type Definition } from './checksum.ts'
import { ids, workflowIds as w } from './ids.ts'
import { createJob, scenarioOf, tickJob } from './jobs.ts'
import { computePosition } from './position.ts'
import { getState, now, resetState, type MockChangeRequest, type MockJob, type MockMigration } from './state.ts'

afterEach(() => resetState())

const ordersBatch = { schema: 'public', table: 'orders', key_column: 'id', batch_size: 100, scope_where: null }

function addMigration(overrides: Partial<Definition> = {}, databaseId: string = ids.inventaris): MockMigration {
  const definition: Definition = { title: 'Uji', up_sql: 'SELECT 1;', down_sql: 'SELECT 2;', irreversible: false, irreversible_reason: '', mode: 'normal', batch: null, ...overrides }
  const migration: MockMigration = { ...definition, id: crypto.randomUUID(), database_id: databaseId, state: 'frozen', revision: 1, checksum: definitionChecksum(definition), created_by: ids.editor, created_at: now(), updated_at: now(), first_execution_at: null }
  getState().migrations.push(migration)
  return migration
}

function approved(kind: MockChangeRequest['kind'], migration: MockMigration, extra: Partial<MockChangeRequest> = {}): MockChangeRequest {
  const request: MockChangeRequest = {
    id: crypto.randomUUID(),
    database_id: migration.database_id,
    migration_id: migration.id,
    migration_revision: 1,
    checksum: migration.checksum,
    revision: 3,
    kind,
    status: 'approved',
    requested_by: ids.editor,
    target_execution_id: null,
    source_job_id: null,
    expected_position_version: 5,
    approvals: [],
    created_at: now(),
    updated_at: now(),
    ...extra,
  }
  getState().changeRequests.push(request)
  return request
}

const queue = (kind: MockChangeRequest['kind'], migration: MockMigration, extra: Partial<MockChangeRequest> = {}) => createJob(approved(kind, migration, extra), migration)
const requestOf = (job: MockJob) => getState().changeRequests.find((candidate) => candidate.id === job.change_request_id)!

// Mengumpulkan status dan jumlah batch setelah tiap tick untuk membandingkan urutan kejadian
function trail(job: MockJob, ticks: number): string[] {
  const states: string[] = []
  for (let i = 0; i < ticks; i++) {
    tickJob(job)
    states.push(`${job.status}:${job.progress.batches_committed}/${job.progress.batches_reverted}`)
  }
  return states
}

describe('scenarioOf', () => {
  it('membaca komentar mock pada SQL tanpa membedakan huruf besar', () => {
    expect(scenarioOf('SELECT 1;')).toBe('ok')
    expect(scenarioOf('-- mock:fail\nSELECT 1;')).toBe('fail')
    expect(scenarioOf('SELECT 1; --  MOCK:Retry')).toBe('retry')
    expect(scenarioOf("status = 'a' -- mock:partial")).toBe('partial')
    expect(scenarioOf('-- mock:uncertain')).toBe('uncertain')
    expect(scenarioOf('-- mock:lain')).toBe('ok')
  })
})

describe('job normal', () => {
  it('queued, running, lalu succeeded dan eksekusi masuk riwayat sekali', () => {
    const migration = addMigration()
    const job = queue('apply', migration)
    expect(job).toMatchObject({ status: 'queued', attempt: 0, max_attempts: 3, started_at: null, finished_at: null, progress: { batches_committed: 0, total_batches: null } })
    expect(trail(job, 2)).toEqual(['running:0/0', 'succeeded:0/0'])
    expect(job).toMatchObject({ attempt: 1, partial: false, error: null })
    expect(job.started_at).toBeTruthy()
    expect(job.finished_at).toBeTruthy()
    expect(job.attempts).toMatchObject([{ attempt: 1, status: 'succeeded', error: null }])
    expect(requestOf(job)).toMatchObject({ status: 'succeeded', revision: 5 })
    const history = getState().executions.filter((item) => item.job_id === job.id)
    expect(history).toHaveLength(1)
    expect(history[0]).toMatchObject({ id: job.execution_id, operation: 'apply', direction: 'up', position_version: 6, actor: 'editor', sql_snapshot: 'SELECT 1;' })
    expect(migration.first_execution_at).toBeTruthy()
    expect(computePosition(ids.inventaris)).toMatchObject({ version: 6, active_execution_id: job.execution_id, active_migration_id: migration.id })
  })

  it('tick tambahan setelah selesai tidak mengubah apa pun', () => {
    const job = queue('apply', addMigration())
    trail(job, 2)
    const before = JSON.stringify(job)
    trail(job, 3)
    expect(JSON.stringify(job).replace(/"ticks":\d+/, '')).toBe(before.replace(/"ticks":\d+/, ''))
    expect(getState().executions).toHaveLength(6)
  })

  it('gagal SQL: transaksi dibatalkan, tidak masuk riwayat, dan posisi tetap', () => {
    const migration = addMigration({ up_sql: '-- mock:fail\nSELECT 1;' })
    const job = queue('apply', migration)
    expect(trail(job, 3)).toEqual(['running:0/0', 'failed:0/0', 'failed:0/0'])
    expect(job.error).toMatchObject({ code: 'sql_error', sqlstate: '42P01', retryable: false })
    expect(job.error?.message).not.toMatch(/pq:|ERROR:/)
    expect(job.attempts).toMatchObject([{ attempt: 1, status: 'failed', error: { code: 'sql_error' } }])
    expect(requestOf(job).status).toBe('failed')
    expect(getState().executions).toHaveLength(5)
    expect(computePosition(ids.inventaris)).toMatchObject({ version: 5, blocked_by_job_id: null })
    expect(migration.first_execution_at).toBeNull()
  })

  it('kegagalan sementara dicoba ulang dengan attempt baru dan hanya satu eksekusi tercatat', () => {
    const job = queue('apply', addMigration({ up_sql: '-- mock:retry\nSELECT 1;' }))
    expect(trail(job, 4)).toEqual(['running:0/0', 'queued:0/0', 'running:0/0', 'succeeded:0/0'])
    expect(job.attempt).toBe(2)
    expect(job.attempts).toMatchObject([
      { attempt: 1, status: 'failed', error: { code: 'connection_error', retryable: true } },
      { attempt: 2, status: 'succeeded', error: null },
    ])
    expect(getState().executions.filter((item) => item.job_id === job.id)).toHaveLength(1)
  })

  it('hasil commit yang belum pasti menahan database lalu direkonsiliasi menjadi sukses', () => {
    const job = queue('apply', addMigration({ up_sql: '-- mock:uncertain\nSELECT 1;' }))
    expect(trail(job, 3)).toEqual(['running:0/0', 'uncertain:0/0', 'uncertain:0/0'])
    expect(job).toMatchObject({ finished_at: null, error: { code: 'commit_uncertain' }, partial: false })
    expect(requestOf(job).status).toBe('uncertain')
    expect(computePosition(ids.inventaris).blocked_by_job_id).toBe(job.id)
    expect(getState().executions).toHaveLength(5)
    expect(trail(job, 1)).toEqual(['succeeded:0/0'])
    expect(requestOf(job).status).toBe('succeeded')
    expect(getState().executions).toHaveLength(6)
    expect(computePosition(ids.inventaris).blocked_by_job_id).toBeNull()
  })
})

describe('job batch', () => {
  it('satu batch per tick sesuai isi tabel dan total diketahui', () => {
    const job = queue('apply', addMigration({ mode: 'batch', up_sql: "status = 'arsip'", down_sql: "status = 'selesai'", batch: ordersBatch }))
    expect(job.progress.total_batches).toBe(2)
    expect(trail(job, 3)).toEqual(['running:0/0', 'running:1/0', 'succeeded:2/0'])
    expect(job.progress).toEqual({ batches_committed: 2, rows_committed: 137, batches_reverted: 0, rows_reverted: 0, total_batches: 2 })
    const execution = getState().executions.find((item) => item.job_id === job.id)
    expect(execution?.statements.map((statement) => statement.batch)).toEqual([1, 2])
    expect(execution?.statements[0].params).toEqual(['[REDACTED]', '[REDACTED]'])
  })

  it('tabel yang tidak dikenal membuat total batch tidak diketahui sampai selesai', () => {
    const job = queue('apply', addMigration({ mode: 'batch', up_sql: 'a = 1', down_sql: 'a = 0', batch: { ...ordersBatch, table: 'tidak_ada' } }))
    expect(job.progress.total_batches).toBeNull()
    trail(job, 2)
    expect(job.progress.total_batches).toBeNull()
    expect(job.progress.batches_committed).toBe(1)
  })

  it('gagal di batch pertama tidak meninggalkan batch yang ter-commit', () => {
    const job = queue('apply', addMigration({ mode: 'batch', up_sql: "status = 'x' -- mock:fail", down_sql: "status = 'y'", batch: ordersBatch }))
    expect(trail(job, 2)).toEqual(['running:0/0', 'failed:0/0'])
    expect(job.partial).toBe(false)
  })

  it('berhenti di tengah menjadi partial, menahan database, dan tidak masuk riwayat', () => {
    const migration = addMigration({ mode: 'batch', up_sql: "status = 'x' -- mock:partial", down_sql: "status = 'y'", batch: ordersBatch })
    const job = queue('apply', migration)
    expect(trail(job, 4)).toEqual(['running:0/0', 'running:1/0', 'running:2/0', 'partial:2/0'])
    expect(job).toMatchObject({ partial: true, error: { code: 'sql_error' } })
    expect(job.finished_at).toBeTruthy()
    expect(job.progress.rows_committed).toBe(137)
    expect(requestOf(job).status).toBe('partial')
    expect(getState().executions).toHaveLength(5)
    expect(computePosition(ids.inventaris)).toMatchObject({ blocked_by_job_id: job.id, can_rollback: false })
  })

  it('resume melanjutkan batch tersisa dengan identitas eksekusi yang sama lalu mencatat satu eksekusi', () => {
    const migration = addMigration({ mode: 'batch', up_sql: "status = 'x' -- mock:partial", down_sql: "status = 'y'", batch: ordersBatch })
    const source = queue('apply', migration)
    trail(source, 4)
    const resume = queue('resume', migration, { source_job_id: source.id })
    expect(resume.execution_id).toBe(source.execution_id)
    expect(resume.progress.batches_committed).toBe(2)
    expect(trail(resume, 2)).toEqual(['running:2/0', 'succeeded:3/0'])
    expect(resume.progress).toMatchObject({ batches_committed: 3, rows_committed: 237, total_batches: 3 })
    expect(source.resolved).toBe(true)
    const history = getState().executions.filter((item) => item.id === source.execution_id)
    expect(history).toHaveLength(1)
    expect(history[0]).toMatchObject({ operation: 'apply', job_id: resume.id })
    expect(history[0].statements).toHaveLength(3)
    expect(computePosition(ids.inventaris).blocked_by_job_id).toBeNull()
  })

  it('revert batch membatalkan dalam urutan terbalik lalu mencatat eksekusi down tanpa rollback', () => {
    const migration = addMigration({ mode: 'batch', up_sql: "status = 'x' -- mock:partial", down_sql: "status = 'y'", batch: ordersBatch })
    const source = queue('apply', migration)
    trail(source, 4)
    const revert = queue('revert_batches', migration, { source_job_id: source.id })
    expect(revert.execution_id).not.toBe(source.execution_id)
    tickJob(revert)
    tickJob(revert)
    expect(revert.progress).toMatchObject({ batches_committed: 2, batches_reverted: 1, rows_reverted: 37 })
    tickJob(revert)
    expect(revert).toMatchObject({ status: 'succeeded' })
    expect(revert.progress).toMatchObject({ batches_reverted: 2, rows_reverted: 137, batches_committed: 2 })
    expect(source.resolved).toBe(true)
    expect(getState().executions.find((item) => item.id === revert.execution_id)).toMatchObject({ operation: 'revert_batches', direction: 'down', reverts_execution_id: null })
    expect(computePosition(ids.inventaris)).toMatchObject({ blocked_by_job_id: null, active_execution_id: w.execution(5) })
  })

  it('pembatalan yang berhenti di tengah menjadi partially_reverted dan tetap menahan database', () => {
    const migration = addMigration({ mode: 'batch', up_sql: "status = 'x' -- mock:partial", down_sql: "status = 'y' -- mock:partial", batch: ordersBatch })
    const source = queue('apply', migration)
    trail(source, 4)
    const revert = queue('revert_batches', migration, { source_job_id: source.id })
    expect(trail(revert, 3)).toEqual(['running:2/0', 'running:2/1', 'partially_reverted:2/1'])
    expect(revert).toMatchObject({ partial: true, error: { code: 'sql_error' } })
    expect(requestOf(revert).status).toBe('partially_reverted')
    expect(computePosition(ids.inventaris).blocked_by_job_id).toBe(revert.id)
    const resume = queue('resume', migration, { source_job_id: revert.id })
    trail(resume, 3)
    expect(resume.status).toBe('succeeded')
    expect(resume.progress.batches_reverted).toBe(2)
    expect(revert.resolved).toBe(true)
    expect(source.resolved).toBe(true)
    expect(computePosition(ids.inventaris).blocked_by_job_id).toBeNull()
  })
})

describe('rollback dan forward', () => {
  it('rollback memakai SQL down asli dan memindahkan eksekusi ke redo chain', () => {
    const migrationC = getState().migrations.find((migration) => migration.id === w.migrationC)!
    const job = queue('rollback', migrationC, { target_execution_id: w.execution(5), expected_position_version: 5 })
    expect(job.plan).toMatchObject({ direction: 'down', sql: "status = 'selesai'" })
    trail(job, 4)
    expect(job.status).toBe('succeeded')
    expect(getState().executions.find((item) => item.id === job.execution_id)).toMatchObject({ operation: 'rollback', direction: 'down', reverts_execution_id: w.execution(5), sql_snapshot: "status = 'selesai'" })
    expect(computePosition(ids.inventaris)).toMatchObject({ active_execution_id: w.execution(4), can_forward: true, forward_execution_id: w.execution(5), forward_migration_id: w.migrationC })
  })

  it('forward memakai SQL up asli dan merujuk eksekusi UP yang diulang', () => {
    const migrationC = getState().migrations.find((migration) => migration.id === w.migrationC)!
    trail(queue('rollback', migrationC, { target_execution_id: w.execution(5) }), 4)
    const forward = queue('forward', migrationC, { target_execution_id: w.execution(5) })
    expect(forward.plan).toMatchObject({ direction: 'up', sql: "status = 'arsip'" })
    trail(forward, 4)
    expect(getState().executions.find((item) => item.id === forward.execution_id)).toMatchObject({ operation: 'forward', direction: 'up', forward_of_execution_id: w.execution(5) })
    expect(computePosition(ids.inventaris)).toMatchObject({ active_execution_id: forward.execution_id, can_forward: false, can_rollback: true })
  })

  it('rollback yang gagal tidak mengubah posisi aktif', () => {
    const migrationC = getState().migrations.find((migration) => migration.id === w.migrationC)!
    migrationC.down_sql = "status = 'x' -- mock:fail"
    const job = queue('rollback', migrationC, { target_execution_id: w.execution(5) })
    trail(job, 3)
    expect(job.status).toBe('failed')
    expect(computePosition(ids.inventaris)).toMatchObject({ version: 5, active_execution_id: w.execution(5) })
  })
})
