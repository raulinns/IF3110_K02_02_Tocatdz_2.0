import { useCallback, useEffect, useMemo, type ReactNode } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { api } from '../api/client.ts'
import { isApiError } from '../api/errors.ts'
import { endSession, sessionKey } from './session-cache.ts'
import { SessionContext, type Session, type SessionState } from './session-context.ts'

// 401 pada /auth/me berarti belum masuk dan bukan error
async function fetchSession(): Promise<Session | null> {
  try {
    return await api.get<Session>('/auth/me', { handleUnauthenticated: false })
  } catch (err) {
    if (isApiError(err) && err.status === 401) return null
    throw err
  }
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
      await api.post('/auth/logout', undefined, { handleUnauthenticated: false })
    } catch (err) {
      // Sesi yang sudah tidak berlaku di server tetap dianggap keluar
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
