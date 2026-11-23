import { api } from '../api/client.ts'
import type { Session } from './session-context.ts'

export interface Credentials {
  username: string
  password: string
}

// 401 pada login adalah hasil yang wajar sehingga tidak boleh memicu pengalihan sesi berakhir
export function login(credentials: Credentials) {
  return api.post<Session>('/auth/login', credentials, { handleUnauthenticated: false })
}
