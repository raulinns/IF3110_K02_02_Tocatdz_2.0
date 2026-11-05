import { useCallback, useEffect, useMemo, type ReactNode } from 'react'
import { useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import { api } from '../api/client.ts'
import { isApiError } from '../api/errors.ts'
import { SessionContext, type Session, type SessionState } from './session-context.ts'

const sessionKey = ['session']

// Mode dev tanpa backend: `VITE_FAKE_SESSION=admin|user npm run dev`.
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

// 401 pada /auth/me berarti belum masuk, bukan error.
async function fetchSession(): Promise<Session | null> {
  const fake = fakeSession()
  if (fake) return fake
  try {
    return await api.get<Session>('/auth/me', { handleUnauthenticated: false })
  } catch (err) {
    if (isApiError(err) && err.status === 401) return null
    throw err
  }
}

// Sesi berakhir: buang semua data cache selain sesi, lalu guard route mengalihkan ke /login.
function endSession(client: QueryClient) {
  client.setQueryData(sessionKey, null)
  client.removeQueries({ predicate: (query) => query.queryKey[0] !== sessionKey[0] })
}

export function SessionProvider({ children }: { children: ReactNode }) {
  const client = useQueryClient()
  const query = useQuery({ queryKey: sessionKey, queryFn: fetchSession, staleTime: Infinity })
  const session = query.data
  const refetch = query.refetch

  useEffect(() => {
    api.configure({
      csrfToken: session?.csrf_token ?? null,
      onUnauthenticated: () => endSession(client),
      refreshCsrf: async () => {
        const next = await client.fetchQuery({ queryKey: sessionKey, queryFn: fetchSession, staleTime: 0 })
        return next?.csrf_token ?? null
      },
    })
  }, [client, session?.csrf_token])

  const logout = useCallback(async () => {
    try {
      if (!fakeSession()) await api.post('/auth/logout', undefined, { handleUnauthenticated: false })
    } catch (err) {
      // Sesi yang sudah tidak berlaku di server tetap dianggap keluar.
      if (!isApiError(err) || err.status !== 401) throw err
    }
    endSession(client)
  }, [client])

  const retry = useCallback(() => void refetch(), [refetch])

  const state = useMemo<SessionState>(() => {
    if (query.isPending) return { status: 'loading' }
    if (query.isError) return { status: 'error', error: query.error, retry }
    if (!session) return { status: 'anonymous' }
    return { status: 'authenticated', session, logout }
  }, [query.isPending, query.isError, query.error, session, logout, retry])

  return <SessionContext.Provider value={state}>{children}</SessionContext.Provider>
}
