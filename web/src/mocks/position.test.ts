import { afterEach, describe, expect, it } from 'vitest'
import fixtureText from '../../../api/examples/history-hide-rollback.json?raw'
import type { Schemas } from '../api/types.ts'
import { definitionChecksum } from './checksum.ts'
import { ids, workflowIds as w } from './ids.ts'
import { toExecutionSummary } from './models.ts'
import { blockingJob, computePosition, databaseExecutions, visibleExecutions, walkHistory } from './position.ts'
import { getState, resetState, type MockExecution } from './state.ts'

afterEach(() => resetState())

const fixture = JSON.parse(fixtureText) as { full: { items: Schemas['ExecutionSummary'][] }; hidden: { items: Schemas['ExecutionSummary'][] }; expected_hidden_execution_ids: string[] }
const newestFirst = () => [...databaseExecutions(ids.inventaris)].reverse()

function execution(n: number, operation: MockExecution['operation'], migrationId: string, extra: Partial<MockExecution> = {}): MockExecution {
  return {
    id: `e${n}`,
    database_id: ids.inventaris,
    migration_id: migrationId,
    migration_revision: 1,
    checksum: 'x',
    change_request_id: null,
    job_id: null,
    operation,
    direction: operation === 'rollback' ? 'down' : 'up',
    status: 'committed',
    reverts_execution_id: null,
    forward_of_execution_id: null,
    sql_snapshot: '',
    actor_id: ids.editor,
    actor: 'editor',
    started_at: '',
    committed_at: '',
    position_version: n,
    statements: [],
    ...extra,
  }
}

describe('seed riwayat E1 sampai E5', () => {
  it('sama dengan fixture kontrak pada setiap field yang dibandingkan', () => {
    const fields = ['id', 'database_id', 'migration_id', 'migration_revision', 'change_request_id', 'job_id', 'operation', 'direction', 'status', 'reverts_execution_id', 'forward_of_execution_id', 'actor_id', 'started_at', 'committed_at', 'position_version'] as const
    const seeded = newestFirst().map((item) => Object.fromEntries(fields.map((field) => [field, toExecutionSummary(item)[field]])))
    const expected = fixture.full.items.map((item) => Object.fromEntries(fields.map((field) => [field, item[field]])))
    expect(seeded).toEqual(expected)
  })

  it('Hide rollback menyembunyikan E2 dan E3 saja, dan forward tetap tampil', () => {
    const visible = visibleExecutions(newestFirst(), true).map((item) => item.id)
    expect(visible).toEqual(fixture.hidden.items.map((item) => item.id))
    const hidden = newestFirst().filter((item) => !visible.includes(item.id))
    expect(hidden.map((item) => item.id).sort()).toEqual([...fixture.expected_hidden_execution_ids].sort())
    expect(visible).toContain(w.execution(4))
  })

  it('tanpa filter semua eksekusi tampil dan tidak ada yang dihapus', () => {
    expect(visibleExecutions(newestFirst(), false)).toHaveLength(5)
    expect(getState().executions).toHaveLength(5)
  })

  it('jumlah baris batch seed sama dengan isi tabel orders', () => {
    const orders = getState().tables.find((table) => table.name === 'orders')
    const job = getState().jobs.find((candidate) => candidate.id === w.job(5))
    expect(job?.progress.rows_committed).toBe(orders?.rows.length)
  })

  it('checksum migration seed dihitung dari definisinya', () => {
    for (const migration of getState().migrations) expect(migration.checksum).toBe(definitionChecksum(migration))
  })
})

describe('computePosition', () => {
  it('posisi awal: eksekusi E5 aktif, bisa rollback, belum bisa forward, dan tidak terblokir', () => {
    expect(computePosition(ids.inventaris)).toEqual({
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

  it('database tanpa riwayat berada di posisi kosong', () => {
    expect(computePosition(ids.pelaporan)).toMatchObject({ version: 0, active_execution_id: null, can_rollback: false, can_forward: false })
  })

  it('migration irreversible di puncak tidak bisa di-rollback', () => {
    getState().migrations.find((migration) => migration.id === w.migrationC)!.irreversible = true
    expect(computePosition(ids.inventaris)).toMatchObject({ can_rollback: false, rollback_execution_id: null })
  })

  it('job yang berjalan atau sisa pekerjaan menahan database', () => {
    const state = getState()
    const base = { ...state.jobs[0], id: 'blocker', queued_at: '2026-11-11T00:00:00Z', resolved: false }
    state.jobs.push({ ...base, status: 'running' })
    expect(computePosition(ids.inventaris)).toMatchObject({ blocked_by_job_id: 'blocker', can_rollback: false, can_forward: false })
    state.jobs.at(-1)!.status = 'partial'
    expect(blockingJob(ids.inventaris)?.id).toBe('blocker')
    state.jobs.at(-1)!.resolved = true
    expect(computePosition(ids.inventaris)).toMatchObject({ blocked_by_job_id: null, can_rollback: true })
  })

  it('barrier yang belum dipulihkan didahulukan atas job yang sedang berjalan', () => {
    const state = getState()
    const base = { ...state.jobs[0], resolved: false }
    state.jobs.push({ ...base, id: 'barrier', status: 'uncertain', queued_at: '2026-11-11T00:00:00Z' }, { ...base, id: 'running', status: 'running', queued_at: '2026-11-12T00:00:00Z' })
    expect(blockingJob(ids.inventaris)?.id).toBe('barrier')
  })
})

describe('walkHistory', () => {
  it('rollback C lalu B mengisi redo chain berurutan, dan forward mengembalikannya satu per satu', () => {
    const history = [
      execution(1, 'apply', 'A'),
      execution(2, 'apply', 'B'),
      execution(3, 'apply', 'C'),
      execution(4, 'rollback', 'C', { reverts_execution_id: 'e3' }),
      execution(5, 'rollback', 'B', { reverts_execution_id: 'e2' }),
    ]
    let { stack, redo } = walkHistory(history)
    expect(stack.map((item) => item.id)).toEqual(['e1'])
    expect(redo.map((item) => item.id)).toEqual(['e2', 'e3'])
    history.push(execution(6, 'forward', 'B', { forward_of_execution_id: 'e2' }))
    ;({ stack, redo } = walkHistory(history))
    expect(stack.map((item) => item.id)).toEqual(['e1', 'e6'])
    expect(redo.map((item) => item.id)).toEqual(['e3'])
  })

  it('rollback eksekusi forward mengembalikan eksekusi UP aslinya ke redo chain', () => {
    const history = [
      execution(1, 'apply', 'A'),
      execution(2, 'apply', 'B'),
      execution(3, 'rollback', 'B', { reverts_execution_id: 'e2' }),
      execution(4, 'forward', 'B', { forward_of_execution_id: 'e2' }),
      execution(5, 'rollback', 'B', { reverts_execution_id: 'e4' }),
    ]
    const { stack, redo } = walkHistory(history)
    expect(stack.map((item) => item.id)).toEqual(['e1'])
    expect(redo.map((item) => item.id)).toEqual(['e2'])
  })

  it('eksekusi revert batch tanpa rollback tidak menggeser posisi', () => {
    const history = [execution(1, 'apply', 'A'), execution(2, 'revert_batches', 'B', { direction: 'down' })]
    expect(walkHistory(history).stack.map((item) => item.id)).toEqual(['e1'])
  })
})
