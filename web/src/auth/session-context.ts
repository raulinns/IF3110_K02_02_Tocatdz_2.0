import { createContext, useContext } from 'react'

// Respons GET /api/v1/auth/me.
export interface Session {
  user: { id: string; username: string }
  platform_admin: boolean
  csrf_token: string
  leader_of: string[]
}

export type SessionState =
  | { status: 'loading' }
  | { status: 'error'; error: unknown; retry: () => void }
  | { status: 'anonymous' }
  | { status: 'authenticated'; session: Session; logout: () => Promise<void> }

export const SessionContext = createContext<SessionState | null>(null)

export function useSession(): SessionState {
  const state = useContext(SessionContext)
  if (!state) throw new Error('useSession harus dipakai di dalam SessionProvider')
  return state
}
