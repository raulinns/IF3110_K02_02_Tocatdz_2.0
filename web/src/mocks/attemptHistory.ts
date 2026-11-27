import type { HistoryEntry } from '../api/history.ts'
import { toExecutionSummary } from './models.ts'
import { databaseExecutions, visibleExecutions } from './position.ts'
import { getState } from './state.ts'

const identities = new WeakMap<object, string>()
function identity(attempt: object) {
  let id = identities.get(attempt)
  if (!id) { id = crypto.randomUUID(); identities.set(attempt, id) }
  return id
}

// The simulation projects attempts separately from its committed position stack.
export function attemptHistory(db: string, hide: boolean): HistoryEntry[] {
  const state = getState()
  const executions = databaseExecutions(db)
  const visible = new Set(visibleExecutions(executions, hide).map((entry) => entry.id))
  const result: HistoryEntry[] = []
  for (const job of state.jobs.filter((job) => job.database_id === db)) {
    const definition = state.migrations.find((entry) => entry.id === job.migration_id)!
    const request = state.changeRequests.find((entry) => entry.id === job.change_request_id)!
    for (const attempt of job.attempts) {
      const proven = attempt.status === 'succeeded' || (attempt.status === 'uncertain' && job.status === 'succeeded' && attempt.attempt === job.attempt)
      const committed = proven ? executions.find((entry) => entry.job_id === job.id && entry.id === job.execution_id) : undefined
      if (committed && !visible.has(committed.id)) continue
      result.push({
        id: identity(attempt), database_id: db, migration_id: definition.id, migration_revision: request.migration_revision,
        checksum: request.checksum, change_request_id: request.id, job_id: job.id, execution_id: job.execution_id,
        retry_of_job_id: request.retry_of_job_id ?? null, attempt: attempt.attempt, operation: job.kind,
        direction: job.plan.direction, status: attempt.status, resolved_status: committed ? 'succeeded' : attempt.status,
        actor: state.users.find((user) => user.id === request.requested_by)?.username ?? '', actor_id: request.requested_by,
        started_at: attempt.started_at, finished_at: attempt.finished_at, committed_at: committed?.committed_at ?? null,
        position_version: committed?.position_version ?? null, reverts_execution_id: committed?.reverts_execution_id ?? null,
        forward_of_execution_id: committed?.forward_of_execution_id ?? null, error: attempt.error,
        can_retry: attempt.status === 'failed' && job.status === 'failed' && job.attempt === attempt.attempt && job.mode === 'normal'
          && ['apply', 'rollback', 'forward'].includes(job.kind)
          && !state.changeRequests.some((entry) => entry.retry_of_job_id === job.id && entry.status !== 'rejected'),
        snapshot_available: true, up_sql: definition.up_sql, down_sql: definition.down_sql, sql_snapshot: job.plan.sql,
      })
    }
  }
  for (const execution of executions) {
    if (!visible.has(execution.id) || state.jobs.some((job) => job.id === execution.job_id && job.execution_id === execution.id && job.status === 'succeeded' && job.attempts.length > 0)) continue
    result.push({ ...toExecutionSummary(execution), execution_id: execution.id, attempt: 0, retry_of_job_id: null,
      status: 'succeeded', resolved_status: 'succeeded', finished_at: execution.committed_at,
      error: null, can_retry: false, snapshot_available: true, up_sql: null, down_sql: null, sql_snapshot: execution.sql_snapshot })
  }
  return result.sort((a, b) => b.started_at.localeCompare(a.started_at) || b.id.localeCompare(a.id))
}
