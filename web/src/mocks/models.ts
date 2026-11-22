import type { Schemas } from '../api/types.ts'
import { getState, type Engine, type MockConnection, type MockExecution, type MockGroup, type MockJob, type MockMigration, type MockSession, type MockUser } from './state.ts'

function omit<T extends object, K extends keyof T>(value: T, keys: K[]): Omit<T, K> {
  const copy = { ...value }
  for (const key of keys) delete copy[key]
  return copy
}

// Ringkasan daftar tidak memuat badan SQL dan field internal simulasi tidak pernah keluar lewat API
export function toMigrationSummary(migration: MockMigration): Schemas['MigrationSummary'] {
  return omit(migration, ['up_sql', 'down_sql', 'irreversible_reason', 'batch'])
}

export function toJob(job: MockJob): Schemas['Job'] {
  return omit(job, ['attempts', 'plan', 'ticks', 'resolved'])
}

export function toExecution(execution: MockExecution): Schemas['Execution'] {
  return omit(execution, ['statements'])
}

export function toExecutionSummary(execution: MockExecution): Schemas['ExecutionSummary'] {
  return omit(execution, ['statements', 'sql_snapshot'])
}

export function toUser(user: MockUser): Schemas['User'] {
  return { id: user.id, username: user.username, active: user.active, platform_admin: user.platform_admin }
}

export function toGroup(group: MockGroup): Schemas['Group'] {
  return { id: group.id, name: group.name, leader_user_id: group.leader_user_id, member_count: group.members.length }
}

export function toConnection(connection: MockConnection): Schemas['Connection'] {
  return { id: connection.id, name: connection.name, engine: connection.engine }
}

export function authMe(user: MockUser, session: MockSession): Schemas['AuthMe'] {
  const leaderOf = getState()
    .groups.filter((group) => group.leader_user_id === user.id)
    .map((group) => group.id)
  return { user: toUser(user), platform_admin: user.platform_admin, csrf_token: session.csrfToken, leader_of: leaderOf }
}

// Fitur lanjutan hanya tersedia untuk PostgreSQL, sedangkan MySQL tetap dipertahankan untuk explorer
export function capabilities(engine: Engine): Schemas['Database']['capabilities'] {
  const postgres = engine === 'postgresql'
  return { schema_diff: postgres, query_plans: postgres, workflow: postgres, batch: postgres }
}
