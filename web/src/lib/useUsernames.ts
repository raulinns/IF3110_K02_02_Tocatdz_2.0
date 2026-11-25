import { useQuery } from '@tanstack/react-query'
import { allUsersQuery } from '../api/admin.ts'
import { useSession } from '../auth/session-context.ts'

// Kontrak hanya memberi ID user pada permintaan dan approval sehingga nama hanya bisa diselesaikan untuk diri sendiri dan oleh admin
export function useUsernames(): (id: string | null) => string {
  const state = useSession()
  const me = state.status === 'authenticated' ? state.session : null
  const users = useQuery({ ...allUsersQuery(), enabled: me?.platform_admin === true })
  return (id) => {
    if (!id) return '-'
    if (me && id === me.user.id) return `${me.user.username} (Anda)`
    return users.data?.find((user) => user.id === id)?.username ?? `pengguna ${id.slice(0, 8)}`
  }
}
