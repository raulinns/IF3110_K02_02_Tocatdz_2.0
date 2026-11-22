import type { Schemas } from '../api/types.ts'
import { getState, now, type MockChangeRequest, type MockExecution, type MockJob, type MockMigration } from './state.ts'
import { buildStatements } from './statements.ts'

const barrierStatuses = new Set<MockJob['status']>(['partial', 'partially_reverted', 'uncertain'])
const inFlightStatuses = new Set<MockJob['status']>(['queued', 'running'])

// Eksekusi satu database terurut dari yang paling lama menurut versi posisinya
export function databaseExecutions(databaseId: string): MockExecution[] {
  return getState()
    .executions.filter((execution) => execution.database_id === databaseId)
    .sort((a, b) => a.position_version - b.position_version)
}

// Stack aktif bergerak linear dan rollback memindahkan eksekusi teratas ke awal redo chain yang diisi eksekusi UP aslinya
export function walkHistory(executions: MockExecution[]): { stack: MockExecution[]; redo: MockExecution[] } {
  const stack: MockExecution[] = []
  const redo: MockExecution[] = []
  for (const execution of executions) {
    if (execution.operation === 'rollback' && execution.reverts_execution_id) {
      const index = stack.findIndex((candidate) => candidate.id === execution.reverts_execution_id)
      if (index < 0) continue
      const [undone] = stack.splice(index, 1)
      redo.unshift(executions.find((candidate) => candidate.id === undone.forward_of_execution_id) ?? undone)
    } else if (execution.operation === 'forward') {
      redo.shift()
      stack.push(execution)
    } else if (execution.direction === 'up') {
      stack.push(execution)
    }
  }
  return { stack, redo }
}

// Job yang menahan database: sisa pekerjaan yang belum pasti atau belum dipulihkan lebih dulu, lalu job yang sedang berjalan, dan yang terbaru dibuat didahulukan
export function blockingJob(databaseId: string): MockJob | undefined {
  const jobs = getState()
    .jobs.filter((job) => job.database_id === databaseId)
    .reverse()
  return jobs.find((job) => barrierStatuses.has(job.status) && !job.resolved) ?? jobs.find((job) => inFlightStatuses.has(job.status))
}

export function bumpPosition(databaseId: string): number {
  const versions = getState().positionVersions
  versions[databaseId] = (versions[databaseId] ?? 0) + 1
  return versions[databaseId]
}

export function computePosition(databaseId: string): Schemas['Position'] {
  const { stack, redo } = walkHistory(databaseExecutions(databaseId))
  const top = stack.at(-1)
  const next = redo[0]
  const blocker = blockingJob(databaseId)
  const topMigration = top ? getState().migrations.find((migration) => migration.id === top.migration_id) : undefined
  const canRollback = Boolean(top && topMigration && !topMigration.irreversible && !blocker)
  const canForward = Boolean(next && !blocker)
  return {
    version: getState().positionVersions[databaseId] ?? 0,
    active_execution_id: top?.id ?? null,
    active_migration_id: top?.migration_id ?? null,
    can_rollback: canRollback,
    rollback_execution_id: canRollback && top ? top.id : null,
    can_forward: canForward,
    forward_execution_id: canForward && next ? next.id : null,
    forward_migration_id: canForward && next ? next.migration_id : null,
    blocked_by_job_id: blocker?.id ?? null,
  }
}

// Hide rollback memasangkan eksekusi dengan rollback yang membatalkannya lewat ID eksekusi dan bukan nama migration
export function visibleExecutions(executions: MockExecution[], hideRollback: boolean): MockExecution[] {
  if (!hideRollback) return executions
  const reverted = new Set(executions.flatMap((execution) => (execution.operation === 'rollback' && execution.reverts_execution_id ? [execution.reverts_execution_id] : [])))
  return executions.filter((execution) => !reverted.has(execution.id) && !(execution.operation === 'rollback' && execution.reverts_execution_id))
}

// Hanya operasi yang berhasil di-commit masuk riwayat, dan satu operasi logis dicatat satu kali
export function appendExecution(job: MockJob, request: MockChangeRequest, migration: MockMigration): MockExecution {
  const state = getState()
  const rollback = request.kind === 'rollback'
  const forward = request.kind === 'forward'
  const compensation = request.kind === 'revert_batches'
  const direction: Schemas['Direction'] = rollback || compensation ? 'down' : 'up'
  const committedAt = now()
  const execution: MockExecution = {
    id: job.execution_id,
    database_id: job.database_id,
    migration_id: migration.id,
    migration_revision: request.migration_revision,
    checksum: request.checksum,
    change_request_id: request.id,
    job_id: job.id,
    operation: request.kind === 'resume' ? 'apply' : request.kind,
    direction,
    status: 'committed',
    reverts_execution_id: rollback ? request.target_execution_id : null,
    forward_of_execution_id: forward ? request.target_execution_id : null,
    sql_snapshot: job.plan.sql,
    actor_id: request.requested_by,
    actor: state.users.find((user) => user.id === request.requested_by)?.username ?? 'unknown',
    started_at: job.started_at ?? committedAt,
    committed_at: committedAt,
    position_version: bumpPosition(job.database_id),
    statements: buildStatements(migration.mode, migration.batch, job.plan.sql, Array.from({ length: job.plan.compensate ? job.plan.batches.length : job.progress.batches_committed }, () => 0)),
  }
  state.executions.push(execution)
  migration.first_execution_at ??= committedAt
  return execution
}
