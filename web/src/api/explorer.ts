import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { api, path } from './client.ts'
import { fetchAllPages } from './pages.ts'
import type { Schemas } from './types.ts'

export const rowLimits = [25, 50, 100, 200] as const
export const defaultRowLimit = 50

const catalogStaleTime = 30_000

// Kontrak tidak punya endpoint satu database sehingga seluruh daftar yang boleh diakses dibaca bertahap
export const databasesQuery = () => queryOptions({ queryKey: ['databases'], queryFn: ({ signal }) => fetchAllPages<Schemas['Database']>('/databases', signal), staleTime: catalogStaleTime })

export const schemaQuery = (db: string) =>
  queryOptions({
    queryKey: ['databases', db, 'schema'],
    queryFn: ({ signal }) => api.get<Schemas['SchemaCatalog']>(path`/databases/${db}/schema`, { signal }),
    staleTime: catalogStaleTime,
  })

export const tableQuery = (db: string, schema: string, table: string) =>
  queryOptions({
    queryKey: ['databases', db, 'tables', schema, table],
    queryFn: ({ signal }) => api.get<Schemas['Table']>(path`/databases/${db}/tables/${schema}/${table}`, { signal }),
    staleTime: catalogStaleTime,
  })

// Halaman sebelumnya tetap tampil selama halaman berikutnya dimuat supaya tabel tidak berkedip
export const rowsQuery = (db: string, schema: string, table: string, page: number, limit: number) =>
  queryOptions({
    queryKey: ['databases', db, 'rows', schema, table, { page, limit }],
    queryFn: ({ signal }) => api.get<Schemas['RowsPage']>(path`/databases/${db}/tables/${schema}/${table}/rows`, { query: { page, limit }, signal }),
    placeholderData: keepPreviousData,
  })
