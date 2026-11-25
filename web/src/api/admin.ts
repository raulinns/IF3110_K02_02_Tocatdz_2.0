import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { api, path } from './client.ts'
import { fetchAllPages } from './pages.ts'
import type { Schemas } from './types.ts'

interface Page<T> {
  items: T[]
  page: number
  limit: number
  has_next: boolean
}

export const adminPageSize = 50

// Halaman sebelumnya tetap tampil selama halaman berikutnya dimuat supaya tabel tidak berkedip
export const usersQuery = (page: number) =>
  queryOptions({
    queryKey: ['admin', 'users', { page }],
    queryFn: ({ signal }) => api.get<Page<Schemas['User']>>('/admin/users', { query: { page, limit: adminPageSize }, signal }),
    placeholderData: keepPreviousData,
  })

export const connectionsQuery = (page: number) =>
  queryOptions({
    queryKey: ['admin', 'connections', { page }],
    queryFn: ({ signal }) => api.get<Page<Schemas['Connection']>>('/admin/connections', { query: { page, limit: adminPageSize }, signal }),
    placeholderData: keepPreviousData,
  })

export const groupsQuery = (page: number) =>
  queryOptions({
    queryKey: ['admin', 'groups', { page }],
    queryFn: ({ signal }) => api.get<Page<Schemas['Group']>>('/admin/groups', { query: { page, limit: adminPageSize }, signal }),
    placeholderData: keepPreviousData,
  })

export const grantsQuery = (page: number) =>
  queryOptions({
    queryKey: ['admin', 'grants', { page }],
    queryFn: ({ signal }) => api.get<Page<Schemas['Grant']>>('/admin/grants', { query: { page, limit: adminPageSize }, signal }),
    placeholderData: keepPreviousData,
  })

// Kontrak tidak punya endpoint satu group atau pencarian user sehingga daftar lengkapnya dibaca bertahap
export const allUsersQuery = () => queryOptions({ queryKey: ['admin', 'users', 'all'], queryFn: ({ signal }) => fetchAllPages<Schemas['User']>('/admin/users', signal) })

export const allGroupsQuery = () => queryOptions({ queryKey: ['admin', 'groups', 'all'], queryFn: ({ signal }) => fetchAllPages<Schemas['Group']>('/admin/groups', signal) })

export const allConnectionsQuery = () => queryOptions({ queryKey: ['admin', 'connections', 'all'], queryFn: ({ signal }) => fetchAllPages<Schemas['Connection']>('/admin/connections', signal) })

export const groupMembersQuery = (groupId: string) =>
  queryOptions({ queryKey: ['admin', 'groups', groupId, 'members'], queryFn: ({ signal }) => fetchAllPages<Schemas['User']>(path`/admin/groups/${groupId}/members`, signal) })

export const createUser = (input: Schemas['CreateUserInput']) => api.post<Schemas['User']>('/admin/users', input)

export const updateUser = (id: string, input: Schemas['UpdateUserInput']) => api.patch<Schemas['User']>(path`/admin/users/${id}`, input)

export const createConnection = (input: Schemas['ConnectionInput']) => api.post<Schemas['Connection']>('/admin/connections', input)

export const deleteConnection = (id: string) => api.delete<void>(path`/admin/connections/${id}`)

export const createGroup = (input: Schemas['GroupInput']) => api.post<Schemas['Group']>('/admin/groups', input)

export const renameGroup = (id: string, input: Schemas['GroupInput']) => api.patch<Schemas['Group']>(path`/admin/groups/${id}`, input)

export const deleteGroup = (id: string) => api.delete<void>(path`/admin/groups/${id}`)

export const addMember = (groupId: string, userId: string) => api.put<void>(path`/admin/groups/${groupId}/members/${userId}`)

export const removeMember = (groupId: string, userId: string) => api.delete<void>(path`/admin/groups/${groupId}/members/${userId}`)

export const setLeader = (groupId: string, userId: string) => api.put<Schemas['Group']>(path`/admin/groups/${groupId}/leader`, { user_id: userId })

export const saveGrant = (groupId: string, databaseId: string, input: Schemas['GrantInput']) => api.put<Schemas['Grant']>(path`/admin/grants/${groupId}/${databaseId}`, input)

export const revokeGrant = (groupId: string, databaseId: string) => api.delete<void>(path`/admin/grants/${groupId}/${databaseId}`)
