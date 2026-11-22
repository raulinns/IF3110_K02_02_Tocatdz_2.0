import { describe, expect, it } from 'vitest'
import type { Schemas } from '../../api/types.ts'
import { ids, workflowIds as w } from '../ids.ts'
import { failureOf, setupMockServer, signIn } from '../testing.ts'
import { authHandlers } from './auth.ts'
import { historyHandlers } from './history.ts'

setupMockServer(...authHandlers, ...historyHandlers)

type Page<T> = { items: T[]; page: number; limit: number; has_next: boolean }

const base = `/databases/${ids.inventaris}`
const idsOf = (page: Page<Schemas['ExecutionSummary']>) => page.items.map((item) => item.id)

describe('GET executions', () => {
  it('riwayat terbaru lebih dulu tanpa badan SQL', async () => {
    const { client } = await signIn('reviewer')
    const page = await client.get<Page<Schemas['ExecutionSummary']>>(`${base}/executions`)
    expect(idsOf(page)).toEqual([5, 4, 3, 2, 1].map(w.execution))
    expect(page.items[2]).toMatchObject({ operation: 'rollback', direction: 'down', reverts_execution_id: w.execution(2), position_version: 3, status: 'committed' })
    expect(page.items[1]).toMatchObject({ operation: 'forward', forward_of_execution_id: w.execution(2) })
    expect(JSON.stringify(page)).not.toContain('sql_snapshot')
  })

  it('Hide rollback menyembunyikan E2 dan E3 sedangkan forward tetap tampil', async () => {
    const { client } = await signIn('observer')
    const hidden = await client.get<Page<Schemas['ExecutionSummary']>>(`${base}/executions`, { query: { hide_rollback: true } })
    expect(idsOf(hidden)).toEqual([5, 4, 1].map(w.execution))
    const shown = await client.get<Page<Schemas['ExecutionSummary']>>(`${base}/executions`, { query: { hide_rollback: false } })
    expect(idsOf(shown)).toHaveLength(5)
  })

  it('pemasangan rollback dihitung di seluruh riwayat sebelum paginasi', async () => {
    const { client } = await signIn('reviewer')
    const first = await client.get<Page<Schemas['ExecutionSummary']>>(`${base}/executions`, { query: { hide_rollback: true, limit: 2 } })
    expect(idsOf(first)).toEqual([5, 4].map(w.execution))
    expect(first.has_next).toBe(true)
    const second = await client.get<Page<Schemas['ExecutionSummary']>>(`${base}/executions`, { query: { hide_rollback: true, limit: 2, page: 2 } })
    expect(idsOf(second)).toEqual([w.execution(1)])
    expect(second.has_next).toBe(false)
  })

  it('menolak nilai hide_rollback yang bukan boolean, query asing, tanpa akses, dan MySQL', async () => {
    const { client } = await signIn('observer')
    expect((await failureOf(client.get(`${base}/executions`, { query: { hide_rollback: 'ya' } }))).code).toBe('bad_request')
    expect((await failureOf(client.get(`${base}/executions`, { query: { sort: 'id' } }))).code).toBe('bad_request')
    expect((await failureOf(client.get(`/databases/${ids.arsip}/executions`))).status).toBe(422)
    const auditor = await signIn('auditor')
    expect((await failureOf(auditor.client.get(`${base}/executions`))).code).toBe('forbidden')
  })
})

describe('GET position', () => {
  it('posisi aktif dengan kemampuan rollback dan forward', async () => {
    const { client } = await signIn('observer')
    await expect(client.get(`${base}/position`)).resolves.toEqual({
      version: 5,
      active_execution_id: w.execution(5),
      active_migration_id: w.migrationC,
      can_rollback: true,
      rollback_execution_id: w.execution(5),
      can_forward: false,
      forward_execution_id: null,
      forward_migration_id: null,
      blocked_by_job_id: null,
    })
  })

  it('database tanpa riwayat berada di posisi kosong dan akses tetap dibatasi', async () => {
    const reviewer = await signIn('reviewer')
    await expect(reviewer.client.get(`/databases/${ids.pelaporan}/position`)).resolves.toMatchObject({ version: 0, active_execution_id: null, can_rollback: false })
    const auditor = await signIn('auditor')
    expect((await failureOf(auditor.client.get(`${base}/position`))).code).toBe('forbidden')
  })
})

describe('GET execution dan statements', () => {
  it('detail membawa salinan SQL yang dieksekusi sesuai arah', async () => {
    const { client } = await signIn('reviewer')
    const up = await client.get<Schemas['Execution']>(`${base}/executions/${w.execution(1)}`)
    expect(up).toMatchObject({ id: w.execution(1), direction: 'up', sql_snapshot: 'CREATE TABLE public.promo (id bigint PRIMARY KEY, kode text NOT NULL);', actor: 'editor' })
    const down = await client.get<Schemas['Execution']>(`${base}/executions/${w.execution(3)}`)
    expect(down).toMatchObject({ direction: 'down', sql_snapshot: 'ALTER TABLE public.promo DROP COLUMN diskon;' })
  })

  it('statement eksekusi batch berupa satu UPDATE per batch dengan parameter tersamar', async () => {
    const { client } = await signIn('reviewer')
    const page = await client.get<Page<Schemas['ExecutedStatement']>>(`${base}/executions/${w.execution(5)}/statements`)
    expect(page.items.map((statement) => statement.batch)).toEqual([1, 2])
    expect(page.items[0].params).toEqual(['[REDACTED]', '[REDACTED]'])
    expect(page.items[0].query).toContain("UPDATE public.orders SET status = '[REDACTED]' WHERE id IN (SELECT id FROM public.orders WHERE id > $1 AND id <= $2 AND (status = '[REDACTED]') ORDER BY id LIMIT 100) RETURNING id")
    expect(page.items[0].query).not.toContain('arsip')
    const first = await client.get<Page<Schemas['ExecutedStatement']>>(`${base}/executions/${w.execution(5)}/statements`, { query: { limit: 1 } })
    expect(first.has_next).toBe(true)
  })

  it('statement eksekusi normal tanpa nomor batch', async () => {
    const { client } = await signIn('reviewer')
    const page = await client.get<Page<Schemas['ExecutedStatement']>>(`${base}/executions/${w.execution(2)}/statements`)
    expect(page.items).toEqual([{ query: 'ALTER TABLE public.promo ADD COLUMN diskon numeric(5,2) NOT NULL DEFAULT 0', params: [], batch: null }])
  })

  it('eksekusi yang tidak ada atau milik database lain adalah 404', async () => {
    const { client } = await signIn('reviewer')
    expect((await failureOf(client.get(`${base}/executions/70000000-0000-4000-8000-000000000099`))).code).toBe('not_found')
    expect((await failureOf(client.get(`/databases/${ids.pelaporan}/executions/${w.execution(1)}`))).code).toBe('not_found')
    expect((await failureOf(client.get(`${base}/executions/${w.execution(1)}/statements`, { query: { x: 1 } }))).code).toBe('bad_request')
  })
})
