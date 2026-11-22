import type { Schemas } from '../api/types.ts'
import { getState, type Engine, type MockConnection, type MockGroup, type MockSession, type MockUser } from './state.ts'

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
