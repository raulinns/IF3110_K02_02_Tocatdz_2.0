import { useQuery } from '@tanstack/react-query'
import { useParams } from 'react-router-dom'
import { databasesQuery } from '../api/explorer.ts'

// Database yang sedang dibuka beserta hak ubahnya, diambil dari daftar yang sama dengan layout database
export function useDatabase() {
  const { db = '' } = useParams()
  const query = useQuery(databasesQuery())
  const database = query.data?.find((candidate) => candidate.id === db)
  return { db, database, canEdit: database?.access === 'view_edit', pending: query.isPending }
}
