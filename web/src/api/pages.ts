import { api } from './client.ts'

interface Page<T> {
  items: T[]
  has_next: boolean
}

const pageSize = 200
const maxPages = 20

// Daftar yang tidak punya endpoint per item dibaca bertahap sampai habis dengan batas halaman sebagai pengaman
export async function fetchAllPages<T>(target: string, signal: AbortSignal): Promise<T[]> {
  const items: T[] = []
  for (let page = 1; page <= maxPages; page++) {
    const result = await api.get<Page<T>>(target, { query: { page, limit: pageSize }, signal })
    items.push(...result.items)
    if (!result.has_next) break
  }
  return items
}
