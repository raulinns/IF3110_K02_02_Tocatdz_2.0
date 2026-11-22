import { ApiFailure, forbidden, type Context } from './http.ts'
import { accessFor, getState, type MockConnection } from './state.ts'

// Tanpa akses efektif hasilnya forbidden, termasuk untuk database yang tidak ada supaya keberadaannya tidak bocor
export function authorizedConnection({ params, user }: Context, required: 'read' | 'edit' = 'read'): MockConnection {
  const connection = getState().connections.find((candidate) => candidate.id === params.database_id)
  const access = connection ? accessFor(user.id, connection.id) : null
  if (!connection || !access) throw forbidden()
  if (required === 'edit' && access !== 'view_edit') throw forbidden()
  return connection
}

// Migration, approval, job, dan riwayat hanya berlaku untuk database PostgreSQL
export function requireWorkflow(connection: MockConnection) {
  if (connection.engine !== 'postgresql') throw new ApiFailure(422, 'validation_failed', 'Fitur ini hanya tersedia untuk database PostgreSQL')
}
