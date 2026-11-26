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

export const workflowPageSize = 50

// Jeda polling job dalam milidetik, dibuat objek supaya bisa dipercepat saat test
export const pollIntervals = { active: 2000, uncertain: 5000 }

const finalJobStatuses = new Set<Schemas['JobStatus']>(['succeeded', 'failed', 'partial', 'partially_reverted'])

export const isFinalJob = (status: Schemas['JobStatus']) => finalJobStatuses.has(status)

// Job queued dan running dipolling cepat, hasil yang belum pasti dicek lebih jarang, dan status akhir menghentikan polling
export function pollDelay(status: Schemas['JobStatus'] | undefined): number | false {
  if (status === undefined || status === 'queued' || status === 'running') return pollIntervals.active
  return status === 'uncertain' ? pollIntervals.uncertain : false
}

const base = (db: string) => ['databases', db] as const

export const requestsQuery = (db: string, page: number, status?: Schemas['CRStatus']) =>
  queryOptions({
    queryKey: [...base(db), 'change-requests', { page, status }],
    queryFn: ({ signal }) => api.get<Page<Schemas['ChangeRequest']>>(path`/databases/${db}/change-requests`, { query: { page, limit: workflowPageSize, status }, signal }),
    placeholderData: keepPreviousData,
  })

export const requestQuery = (db: string, id: string) =>
  queryOptions({
    queryKey: [...base(db), 'change-requests', 'detail', id],
    queryFn: ({ signal }) => api.get<Schemas['ChangeRequest']>(path`/databases/${db}/change-requests/${id}`, { signal }),
  })

// Judul migration untuk daftar yang hanya membawa ID migration
export const allMigrationsQuery = (db: string) =>
  queryOptions({ queryKey: [...base(db), 'migrations', 'all'], queryFn: ({ signal }) => fetchAllPages<Schemas['MigrationSummary']>(path`/databases/${db}/migrations`, signal) })

export const jobsQuery = (db: string, page: number, status?: Schemas['JobStatus']) =>
  queryOptions({
    queryKey: [...base(db), 'jobs', { page, status }],
    queryFn: ({ signal }) => api.get<Page<Schemas['Job']>>(path`/databases/${db}/jobs`, { query: { page, limit: workflowPageSize, status }, signal }),
    placeholderData: keepPreviousData,
  })

export const jobQuery = (jobId: string) =>
  queryOptions({ queryKey: ['jobs', jobId], queryFn: ({ signal }) => api.get<Schemas['Job']>(path`/jobs/${jobId}`, { signal }) })

export const attemptsQuery = (jobId: string) =>
  queryOptions({ queryKey: ['jobs', jobId, 'attempts'], queryFn: ({ signal }) => fetchAllPages<Schemas['JobAttempt']>(path`/jobs/${jobId}/attempts`, signal) })

export const positionQuery = (db: string) =>
  queryOptions({ queryKey: [...base(db), 'position'], queryFn: ({ signal }) => api.get<Schemas['Position']>(path`/databases/${db}/position`, { signal }) })

export const executionsQuery = (db: string, page: number, hideRollback: boolean) =>
  queryOptions({
    queryKey: [...base(db), 'executions', { page, hideRollback }],
    queryFn: ({ signal }) => api.get<Page<Schemas['ExecutionSummary']>>(path`/databases/${db}/executions`, { query: { page, limit: workflowPageSize, hide_rollback: hideRollback ? 'true' : undefined }, signal }),
    placeholderData: keepPreviousData,
  })

export const executionQuery = (db: string, id: string) =>
  queryOptions({ queryKey: [...base(db), 'executions', 'detail', id], queryFn: ({ signal }) => api.get<Schemas['Execution']>(path`/databases/${db}/executions/${id}`, { signal }), staleTime: Infinity })

// Statement yang dijalankan dimuat per halaman karena satu eksekusi batch bisa memuat ratusan batch
export const statementsPageSize = 25

export const statementsQuery = (db: string, id: string, page: number) =>
  queryOptions({
    queryKey: [...base(db), 'executions', 'statements', id, page],
    queryFn: ({ signal }) => api.get<Page<Schemas['ExecutedStatement']>>(path`/databases/${db}/executions/${id}/statements`, { query: { page, limit: statementsPageSize }, signal }),
    placeholderData: keepPreviousData,
    staleTime: Infinity,
  })

export const decide = (db: string, id: string, input: Schemas['ApprovalInput']) => api.post<Schemas['ChangeRequest']>(path`/databases/${db}/change-requests/${id}/approvals`, input)

export const executeRequest = (db: string, id: string, revision: number) => api.post<Schemas['JobAccepted']>(path`/databases/${db}/change-requests/${id}/execute`, { revision })
