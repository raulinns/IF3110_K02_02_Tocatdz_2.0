import { useCallback, useMemo, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { SessionContext, type Session, type SessionState } from './session-context.ts'

const queryKey = ['session']

// Sementara sebelum FE-02 (mock MSW) dan FE-03 (API client): `VITE_FAKE_SESSION=admin|user npm run dev`
// memakai sesi palsu tanpa backend. Hanya berlaku di mode dev.
function fakeSession(): Session | null {
  const kind = import.meta.env.DEV ? import.meta.env.VITE_FAKE_SESSION : undefined
  if (kind !== 'admin' && kind !== 'user') return null
  return {
    user: { id: '00000000-0000-4000-8000-000000000001', username: kind === 'admin' ? 'admin' : 'pengamat' },
    platform_admin: kind === 'admin',
    csrf_token: 'fake',
    leader_of: [],
  }
}

async function fetchSession(): Promise<Session | null> {
  const fake = fakeSession()
  if (fake) return fake
  const res = await fetch('/api/v1/auth/me', { credentials: 'same-origin', headers: { Accept: 'application/json' } })
  if (res.status === 401) return null
  if (!res.ok) throw new Error(`auth/me: ${res.status}`)
  return (await res.json()) as Session
}

export function SessionProvider({ children }: { children: ReactNode }) {
  const client = useQueryClient()
  const query = useQuery({ queryKey, queryFn: fetchSession, staleTime: Infinity })
  const session = query.data
  const csrf = session?.csrf_token
  const refetch = query.refetch

  const logout = useCallback(async () => {
    if (!fakeSession()) {
      await fetch('/api/v1/auth/logout', {
        method: 'POST',
        credentials: 'same-origin',
        headers: { 'X-CSRF-Token': csrf ?? '' },
      })
    }
    client.clear()
    client.setQueryData(queryKey, null)
  }, [client, csrf])

  const retry = useCallback(() => void refetch(), [refetch])

  const state = useMemo<SessionState>(() => {
    if (query.isPending) return { status: 'loading' }
    if (query.isError) return { status: 'error', retry }
    if (!session) return { status: 'anonymous' }
    return { status: 'authenticated', session, logout }
  }, [query.isPending, query.isError, session, logout, retry])

  return <SessionContext.Provider value={state}>{children}</SessionContext.Provider>
}
