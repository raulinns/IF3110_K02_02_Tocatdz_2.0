import { keepPreviousData, queryOptions } from '@tanstack/react-query'
import { api, path } from './client.ts'
import type { Schemas } from './types.ts'
import { workflowPageSize } from './workflow.ts'

export type HistoryEntry = Schemas['HistoryEntry']
export type HistoryStatement = Schemas['HistoryStatement']

export const historyQuery = (db: string, page: number, hideRollback: boolean) => queryOptions({
  queryKey: ['databases', db, 'executions', 'attempt-history', { page, hideRollback }],
  queryFn: ({ signal }) => api.get<Schemas['HistoryPage']>(path`/databases/${db}/history`, {
    query: { page, limit: workflowPageSize, hide_rollback: hideRollback ? 'true' : undefined }, signal,
  }),
  placeholderData: keepPreviousData,
})
export const historyEntryQuery = (db: string, id: string) => queryOptions({
  queryKey: ['databases', db, 'executions', 'attempt-detail', id],
  queryFn: ({ signal }) => api.get<HistoryEntry>(path`/databases/${db}/history/${id}`, { signal }),
})
export const retryFailedJob = (db: string, id: string) => api.post<Schemas['ChangeRequest']>(path`/databases/${db}/jobs/${id}/retry`, {})
