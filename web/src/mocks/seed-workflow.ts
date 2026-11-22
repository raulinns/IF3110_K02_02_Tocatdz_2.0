import type { Schemas } from '../api/types.ts'
import { definitionChecksum, type Definition } from './checksum.ts'
import { ids, workflowIds as w } from './ids.ts'
import type { MockChangeRequest, MockExecution, MockJob, MockMigration, MockState } from './state.ts'
import { buildStatements } from './statements.ts'

type WorkflowSeed = Pick<MockState, 'migrations' | 'changeRequests' | 'jobs' | 'executions' | 'positionVersions'>

const at = (minute: number, second = 0) => `2026-11-10T10:${String(minute).padStart(2, '0')}:${String(second).padStart(2, '0')}Z`
const earlier = (n: number, second = 0) => `2026-11-10T09:${50 + n}:${String(second).padStart(2, '0')}Z`

const ordersBatch = { schema: 'public', table: 'orders', key_column: 'id', batch_size: 100, scope_where: "status = 'selesai'" }

const normal = (title: string, up_sql: string, down_sql: string): Definition => ({ title, up_sql, down_sql, irreversible: false, irreversible_reason: '', mode: 'normal', batch: null })

const definitions = {
  a: normal('Buat tabel promo', 'CREATE TABLE public.promo (id bigint PRIMARY KEY, kode text NOT NULL);', 'DROP TABLE public.promo;'),
  b: normal('Tambah kolom diskon pada promo', 'ALTER TABLE public.promo ADD COLUMN diskon numeric(5,2) NOT NULL DEFAULT 0;', 'ALTER TABLE public.promo DROP COLUMN diskon;'),
  c: { title: 'Arsipkan order selesai', up_sql: "status = 'arsip'", down_sql: "status = 'selesai'", irreversible: false, irreversible_reason: '', mode: 'batch', batch: ordersBatch } satisfies Definition,
  d: normal('Tambah indeks status order', 'CREATE INDEX idx_orders_status ON public.orders (status);', 'DROP INDEX public.idx_orders_status;'),
  e: { title: 'Hapus kolom promo lama', up_sql: 'ALTER TABLE public.promo DROP COLUMN kode;', down_sql: null, irreversible: true, irreversible_reason: 'Data kolom yang dihapus tidak dapat dipulihkan.', mode: 'normal', batch: null } satisfies Definition,
  p: normal('Tambah tabel ringkasan bulanan', 'CREATE TABLE public.ringkasan_bulanan (bulan date PRIMARY KEY, total numeric(14,2) NOT NULL);', 'DROP TABLE public.ringkasan_bulanan;'),
}

function migration(id: string, databaseId: string, definition: Definition, createdAt: string, firstExecutionAt: string | null): MockMigration {
  return {
    ...definition,
    id,
    database_id: databaseId,
    state: firstExecutionAt ? 'frozen' : 'draft',
    revision: 1,
    checksum: definitionChecksum(definition),
    created_by: ids.editor,
    created_at: createdAt,
    updated_at: createdAt,
    first_execution_at: firstExecutionAt,
  }
}

interface HistoryStep {
  n: number
  migration: MockMigration
  operation: Schemas['ChangeKind']
  direction: Schemas['Direction']
  revertsExecution?: string
  forwardOf?: string
}

// Rollback dan forward memakai SQL asli dari migration yang dieksekusi dan batch C berisi 137 baris order dalam 2 batch
const orderBatches = [100, 37]

// Lima eksekusi pertama sama dengan contoh E1 sampai E5 pada spesifikasi dan fixture kontrak
export function workflowSeed(): WorkflowSeed {
  const inventaris = ids.inventaris
  const a = migration(w.migrationA, inventaris, definitions.a, earlier(0), at(1, 1))
  const b = migration(w.migrationB, inventaris, definitions.b, earlier(0), at(2, 1))
  const c = migration(w.migrationC, inventaris, definitions.c, earlier(0), at(5, 1))
  const migrations = [
    a,
    b,
    c,
    migration(w.migrationD, inventaris, definitions.d, '2026-11-10T11:00:00Z', null),
    migration(w.migrationE, inventaris, definitions.e, '2026-11-10T11:05:00Z', null),
    migration(w.migrationP, ids.pelaporan, definitions.p, '2026-11-10T11:10:00Z', null),
  ]
  const steps: HistoryStep[] = [
    { n: 1, migration: a, operation: 'apply', direction: 'up' },
    { n: 2, migration: b, operation: 'apply', direction: 'up' },
    { n: 3, migration: b, operation: 'rollback', direction: 'down', revertsExecution: w.execution(2) },
    { n: 4, migration: b, operation: 'forward', direction: 'up', forwardOf: w.execution(2) },
    { n: 5, migration: c, operation: 'apply', direction: 'up' },
  ]
  const changeRequests: MockChangeRequest[] = []
  const jobs: MockJob[] = []
  const executions: MockExecution[] = []
  for (const step of steps) {
    const { n, migration: target, operation, direction } = step
    const sql = direction === 'up' ? target.up_sql : (target.down_sql ?? '')
    const batchRows = target.mode === 'batch' ? orderBatches : []
    const targetExecution = step.revertsExecution ?? step.forwardOf ?? null
    changeRequests.push({
      id: w.changeRequest(n),
      database_id: inventaris,
      migration_id: target.id,
      migration_revision: 1,
      checksum: target.checksum,
      revision: 6,
      kind: operation,
      status: 'succeeded',
      requested_by: ids.editor,
      target_execution_id: targetExecution,
      source_job_id: null,
      expected_position_version: n - 1,
      approvals: [
        { group_id: ids.writers, group_name: 'writers', access: 'view_edit', leader_user_id: ids.reviewer, decision: 'approve', actor_id: ids.reviewer, note: '', decided_at: earlier(n, 30) },
        { group_id: ids.readers, group_name: 'readers', access: 'view_only', leader_user_id: ids.observer, decision: 'approve', actor_id: ids.observer, note: '', decided_at: earlier(n, 45) },
      ],
      created_at: earlier(n),
      updated_at: at(n, 1),
    })
    jobs.push({
      id: w.job(n),
      database_id: inventaris,
      change_request_id: w.changeRequest(n),
      execution_id: w.execution(n),
      migration_id: target.id,
      kind: operation,
      mode: target.mode,
      status: 'succeeded',
      attempt: 1,
      max_attempts: 3,
      run_after: earlier(n, 50),
      queued_at: earlier(n, 50),
      started_at: at(n),
      finished_at: at(n, 1),
      partial: false,
      progress: { batches_committed: batchRows.length, rows_committed: batchRows.reduce((sum, rows) => sum + rows, 0), batches_reverted: 0, rows_reverted: 0, total_batches: batchRows.length > 0 ? batchRows.length : null },
      error: null,
      attempts: [{ attempt: 1, status: 'succeeded', started_at: at(n), finished_at: at(n, 1), error: null }],
      plan: { direction, sql, scenario: 'ok', batches: batchRows, done: batchRows.length, totalKnown: true, compensate: false, resume: false, uncertainAt: null },
      ticks: 0,
      resolved: false,
    })
    executions.push({
      id: w.execution(n),
      database_id: inventaris,
      migration_id: target.id,
      migration_revision: 1,
      checksum: target.checksum,
      change_request_id: w.changeRequest(n),
      job_id: w.job(n),
      operation,
      direction,
      status: 'committed',
      reverts_execution_id: step.revertsExecution ?? null,
      forward_of_execution_id: step.forwardOf ?? null,
      sql_snapshot: sql,
      actor_id: ids.editor,
      actor: 'editor',
      started_at: at(n),
      committed_at: at(n, 1),
      position_version: n,
      statements: buildStatements(target.mode, target.batch, sql, batchRows),
    })
  }
  return { migrations, changeRequests, jobs, executions, positionVersions: { [inventaris]: steps.length, [ids.pelaporan]: 0, [ids.arsip]: 0 } }
}
