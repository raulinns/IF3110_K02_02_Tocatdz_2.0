import type { QueryClient } from '@tanstack/react-query'
import type { Session } from './session-context.ts'

export const sessionKey = ['session']

// Login berhasil: sesi dari respons langsung dipakai sehingga tidak perlu memanggil /auth/me lagi
export function startSession(client: QueryClient, session: Session) {
  client.setQueryData(sessionKey, session)
}

// Sesi berakhir: buang semua data cache selain sesi lalu guard route mengalihkan ke /login
export function endSession(client: QueryClient) {
  client.setQueryData(sessionKey, null)
  client.removeQueries({ predicate: (query) => query.queryKey[0] !== sessionKey[0] })
}
