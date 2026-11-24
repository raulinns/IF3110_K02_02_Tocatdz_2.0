import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { api, path } from './client.ts'
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

export const createUser = (input: Schemas['CreateUserInput']) => api.post<Schemas['User']>('/admin/users', input)

export const updateUser = (id: string, input: Schemas['UpdateUserInput']) => api.patch<Schemas['User']>(path`/admin/users/${id}`, input)

export const createConnection = (input: Schemas['ConnectionInput']) => api.post<Schemas['Connection']>('/admin/connections', input)

export const deleteConnection = (id: string) => api.delete<void>(path`/admin/connections/${id}`)
