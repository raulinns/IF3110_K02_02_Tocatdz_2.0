import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { api, path } from './client.ts'
import type { Schemas } from './types.ts'

interface Page<T> {
  items: T[]
  page: number
  limit: number
  has_next: boolean
}

export const migrationPageSize = 50

// Bagian migration yang bisa disunting pengguna, sama dengan isi permintaan buat dan simpan tanpa revision
export type Definition = Pick<Schemas['Migration'], 'title' | 'up_sql' | 'down_sql' | 'irreversible' | 'irreversible_reason' | 'mode' | 'batch'>

export interface SchemaDiffParams {
  desired: string
  currentSchema: string
  desiredSchema: string
}

export const migrationsQuery = (db: string, page: number) =>
  queryOptions({
    queryKey: ['databases', db, 'migrations', { page }],
    queryFn: ({ signal }) => api.get<Page<Schemas['MigrationSummary']>>(path`/databases/${db}/migrations`, { query: { page, limit: migrationPageSize }, signal }),
    placeholderData: keepPreviousData,
  })

export const migrationQuery = (db: string, id: string) =>
  queryOptions({
    queryKey: ['databases', db, 'migration', id],
    queryFn: ({ signal }) => api.get<Schemas['Migration']>(path`/databases/${db}/migrations/${id}`, { signal }),
  })

// Hasil perbandingan menggambarkan kondisi saat dibandingkan sehingga selalu dimuat ulang saat dibuka lagi
export const schemaDiffQuery = (db: string, params: SchemaDiffParams) =>
  queryOptions({
    queryKey: ['databases', db, 'schema-diff', params],
    queryFn: ({ signal }) =>
      api.get<Schemas['SchemaDiff']>(path`/databases/${db}/schema-diff`, {
        query: { desired_database_id: params.desired, current_schema: params.currentSchema, desired_schema: params.desiredSchema },
        signal,
      }),
    staleTime: 0,
    gcTime: 0,
    retry: false,
  })

export const createMigration = (db: string, definition: Definition) => api.post<Schemas['Migration']>(path`/databases/${db}/migrations`, definition)

export const updateMigration = (db: string, id: string, definition: Definition, revision: number) =>
  api.patch<Schemas['Migration']>(path`/databases/${db}/migrations/${id}`, { ...definition, revision })

export const explainMigration = (db: string, id: string, input: Schemas['PlanInput']) => api.post<Schemas['Plan']>(path`/databases/${db}/migrations/${id}/plan`, input)
