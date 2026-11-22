import { describe, expect, it } from 'vitest'
import type { Schemas } from '../../api/types.ts'
import { ids, workflowIds as w } from '../ids.ts'
import { failureOf, setupMockServer, signIn } from '../testing.ts'
import { handlers } from './index.ts'

setupMockServer(...handlers)

type Page<T> = { items: T[]; page: number; limit: number; has_next: boolean }

const db = `/databases/${ids.inventaris}`
const act = async (username: string) => (await signIn(username)).client
const ordersBatch = { schema: 'public', table: 'orders', key_column: 'id', batch_size: 100, scope_where: null }
const normal = (title: string, up_sql: string) => ({ title, up_sql, down_sql: 'SELECT 1;', irreversible: false, irreversible_reason: '', mode: 'normal', batch: null })
const batch = (title: string, marker = '') => ({ title, up_sql: `status = 'arsip'${marker}`, down_sql: "status = 'selesai'", irreversible: false, irreversible_reason: '', mode: 'batch', batch: ordersBatch })

async function createMigration(body: unknown) {
  return (await act('editor')).post<Schemas['Migration']>(`${db}/migrations`, body)
}

// Pengaju mengajukan lalu leader writers dan readers menyetujui, seperti alur review yang sebenarnya
async function approve(extra: Record<string, unknown>, migration: Pick<Schemas['Migration'], 'id' | 'revision'>) {
  const pending = await (await act('editor')).post<Schemas['ChangeRequest']>(`${db}/change-requests`, { migration_id: migration.id, migration_revision: migration.revision, kind: 'apply', ...extra })
  const decide = async (user: string, request: Schemas['ChangeRequest'], groupId: string) => (await act(user)).post<Schemas['ChangeRequest']>(`${db}/change-requests/${request.id}/approvals`, { revision: request.revision, group_id: groupId, decision: 'approve' })
  return decide('observer', await decide('reviewer', pending, ids.writers), ids.readers)
}

async function run(request: Schemas['ChangeRequest']) {
  return (await act('editor')).post<Schemas['JobAccepted']>(`${db}/change-requests/${request.id}/execute`, { revision: request.revision })
}

// Polling berhenti pada status yang dicari dan mencatat status serta jumlah batch di setiap pembacaan
async function poll(jobId: string, until: Schemas['JobStatus']) {
  const client = await act('editor')
  const trail: string[] = []
  let job: Schemas['Job'] | undefined
  for (let i = 0; i < 15 && job?.status !== until; i++) {
    job = await client.get<Schemas['Job']>(`/jobs/${jobId}`)
    trail.push(`${job.status}:${job.progress.batches_committed}`)
  }
  return { trail, job: job as Schemas['Job'] }
}

const history = async (query: Record<string, string | number | boolean> = {}) => (await act('editor')).get<Page<Schemas['ExecutionSummary']>>(`${db}/executions`, { query })
const position = async () => (await act('editor')).get<Schemas['Position']>(`${db}/position`)

describe('apply sampai selesai', () => {
  it('draft menjadi migration beku, job berjalan per batch, lalu riwayat dan posisi bertambah', async () => {
    const migration = await createMigration(batch('Arsipkan semua'))
    const request = await approve({}, migration)
    expect(request.status).toBe('approved')
    const accepted = await run(request)
    expect(accepted.status).toBe('queued')
    const { trail, job } = await poll(accepted.job_id, 'succeeded')
    expect(trail).toEqual(['queued:0', 'running:0', 'running:1', 'succeeded:2'])
    expect(job).toMatchObject({ kind: 'apply', mode: 'batch', attempt: 1, partial: false, error: null, progress: { rows_committed: 137, total_batches: 2 } })

    const newest = (await history()).items[0]
    expect(newest).toMatchObject({ id: job.execution_id, migration_id: migration.id, operation: 'apply', position_version: 6, actor: 'editor', job_id: job.id, change_request_id: request.id })
    expect(await position()).toMatchObject({ version: 6, active_execution_id: job.execution_id, active_migration_id: migration.id, can_rollback: true, blocked_by_job_id: null })
    const client = await act('editor')
    await expect(client.get(`${db}/change-requests/${request.id}`)).resolves.toMatchObject({ status: 'succeeded' })
    await expect(client.get(`${db}/migrations/${migration.id}`)).resolves.toMatchObject({ state: 'frozen', first_execution_at: expect.any(String) })
    expect((await failureOf(client.patch(`${db}/migrations/${migration.id}`, { ...batch('Ubah'), revision: 1 }))).reason).toBe('definition_frozen')
    expect((await client.get<Page<Schemas['JobAttempt']>>(`/jobs/${job.id}/attempts`)).items).toMatchObject([{ attempt: 1, status: 'succeeded' }])
  })
})

describe('rollback dan forward', () => {
  it('rollback migration aktif lalu forward mengembalikannya, dan Hide rollback mengikuti ID eksekusi', async () => {
    const migrationC = await (await act('editor')).get<Schemas['Migration']>(`${db}/migrations/${w.migrationC}`)
    const rollback = await approve({ kind: 'rollback', target_execution_id: w.execution(5) }, migrationC)
    const rolled = await poll((await run(rollback)).job_id, 'succeeded')
    const rollbackExecution = (await history()).items[0]
    expect(rollbackExecution).toMatchObject({ id: rolled.job.execution_id, operation: 'rollback', direction: 'down', reverts_execution_id: w.execution(5), position_version: 6 })
    expect(await position()).toMatchObject({ active_execution_id: w.execution(4), can_rollback: true, can_forward: true, forward_execution_id: w.execution(5), forward_migration_id: w.migrationC })
    expect((await history({ hide_rollback: true })).items.map((item) => item.id)).toEqual([w.execution(4), w.execution(1)])
    expect((await failureOf((await act('editor')).post(`${db}/change-requests`, { migration_id: w.migrationD, migration_revision: 1, kind: 'apply' }))).reason).toBe('position_changed')

    const forward = await approve({ kind: 'forward', target_execution_id: w.execution(5) }, migrationC)
    const forwarded = await poll((await run(forward)).job_id, 'succeeded')
    const forwardExecution = (await history()).items[0]
    expect(forwardExecution).toMatchObject({ id: forwarded.job.execution_id, operation: 'forward', forward_of_execution_id: w.execution(5), position_version: 7 })
    expect(await position()).toMatchObject({ active_execution_id: forwarded.job.execution_id, can_forward: false, can_rollback: true })
    expect((await history({ hide_rollback: true })).items.map((item) => item.id)).toEqual([forwardExecution.id, w.execution(4), w.execution(1)])
    expect((await history()).items).toHaveLength(7)
  })

  it('migration irreversible di puncak tidak bisa dibatalkan', async () => {
    const irreversible = await createMigration({ ...normal('Hapus kolom', 'ALTER TABLE public.promo DROP COLUMN kode;'), down_sql: null, irreversible: true, irreversible_reason: 'Data hilang' })
    await poll((await run(await approve({}, irreversible))).job_id, 'succeeded')
    const current = await position()
    expect(current).toMatchObject({ can_rollback: false, rollback_execution_id: null, active_migration_id: irreversible.id })
    const err = await failureOf((await act('editor')).post(`${db}/change-requests`, { migration_id: irreversible.id, migration_revision: 1, kind: 'rollback', target_execution_id: current.active_execution_id }))
    expect(err).toMatchObject({ status: 409, reason: 'irreversible' })
  })
})

describe('jalur kegagalan', () => {
  it('SQL gagal: job failed, tidak masuk riwayat, posisi tetap, dan migration tetap beku', async () => {
    const migration = await createMigration(normal('Akan gagal', '-- mock:fail\nSELECT 1;'))
    const request = await approve({}, migration)
    const { trail, job } = await poll((await run(request)).job_id, 'failed')
    expect(trail).toEqual(['queued:0', 'running:0', 'failed:0'])
    expect(job.error).toMatchObject({ code: 'sql_error', sqlstate: '42P01', retryable: false })
    await expect((await act('editor')).get(`${db}/change-requests/${request.id}`)).resolves.toMatchObject({ status: 'failed' })
    expect((await history()).items).toHaveLength(5)
    expect(await position()).toMatchObject({ version: 5, blocked_by_job_id: null })
    expect((await failureOf((await act('editor')).patch(`${db}/migrations/${migration.id}`, { ...normal('Perbaiki', 'SELECT 1;'), revision: 1 }))).reason).toBe('definition_frozen')
    await expect((await act('editor')).post(`${db}/change-requests`, { migration_id: w.migrationD, migration_revision: 1, kind: 'apply' })).resolves.toMatchObject({ status: 'pending_review' })
  })

  it('kegagalan sementara dicoba ulang otomatis dan hanya satu eksekusi tercatat', async () => {
    const migration = await createMigration(normal('Coba ulang', '-- mock:retry\nSELECT 1;'))
    const { trail, job } = await poll((await run(await approve({}, migration))).job_id, 'succeeded')
    expect(trail).toEqual(['queued:0', 'running:0', 'queued:0', 'running:0', 'succeeded:0'])
    expect(job.attempt).toBe(2)
    const attempts = await (await act('editor')).get<Page<Schemas['JobAttempt']>>(`/jobs/${job.id}/attempts`)
    expect(attempts.items.map((attempt) => [attempt.attempt, attempt.status, attempt.error?.code ?? null])).toEqual([
      [1, 'failed', 'connection_error'],
      [2, 'succeeded', null],
    ])
    expect((await history()).items.filter((item) => item.job_id === job.id)).toHaveLength(1)
  })

  it('hasil commit yang belum pasti menahan database sampai direkonsiliasi', async () => {
    const migration = await createMigration(normal('Belum pasti', '-- mock:uncertain\nSELECT 1;'))
    const { job: held } = await poll((await run(await approve({}, migration))).job_id, 'uncertain')
    expect(held).toMatchObject({ status: 'uncertain', finished_at: null, error: { code: 'commit_uncertain' } })
    expect(await position()).toMatchObject({ blocked_by_job_id: held.id, can_rollback: false })
    const err = await failureOf((await act('editor')).post(`${db}/change-requests`, { migration_id: w.migrationD, migration_revision: 1, kind: 'apply' }))
    expect(err).toMatchObject({ status: 409, reason: 'database_blocked' })
    const { job } = await poll(held.id, 'succeeded')
    expect(job.status).toBe('succeeded')
    expect(await position()).toMatchObject({ blocked_by_job_id: null, active_migration_id: migration.id })
  })
})

describe('partial, resume, dan revert batch', () => {
  it('batch yang berhenti di tengah menahan database lalu resume menyelesaikannya dengan satu eksekusi', async () => {
    const migration = await createMigration(batch('Partial', ' -- mock:partial'))
    const { trail, job: partial } = await poll((await run(await approve({}, migration))).job_id, 'partial')
    expect(trail).toEqual(['queued:0', 'running:0', 'running:1', 'running:2', 'partial:2'])
    expect(partial).toMatchObject({ partial: true, progress: { batches_committed: 2, rows_committed: 137, total_batches: 3 } })
    expect(await position()).toMatchObject({ blocked_by_job_id: partial.id, can_rollback: false })
    expect((await history()).items).toHaveLength(5)

    const resume = await approve({ kind: 'resume', source_job_id: partial.id }, migration)
    expect(resume).toMatchObject({ kind: 'resume', source_job_id: partial.id })
    const resumed = await poll((await run(resume)).job_id, 'succeeded')
    expect(resumed.trail).toEqual(['queued:2', 'running:2', 'succeeded:3'])
    expect(resumed.job).toMatchObject({ execution_id: partial.execution_id, progress: { batches_committed: 3, rows_committed: 237 } })
    expect(await position()).toMatchObject({ blocked_by_job_id: null, active_execution_id: partial.execution_id })
    const executions = (await history()).items.filter((item) => item.id === partial.execution_id)
    expect(executions).toMatchObject([{ operation: 'apply', job_id: resumed.job.id }])
    const statements = await (await act('editor')).get<Page<Schemas['ExecutedStatement']>>(`${db}/executions/${partial.execution_id}/statements`)
    expect(statements.items.map((statement) => statement.batch)).toEqual([1, 2, 3])
  })

  it('revert batch membatalkan batch yang ter-commit dan mencatat eksekusi down tanpa menggeser posisi', async () => {
    const migration = await createMigration(batch('Partial revert', ' -- mock:partial'))
    const { job: partial } = await poll((await run(await approve({}, migration))).job_id, 'partial')
    const revert = await approve({ kind: 'revert_batches', source_job_id: partial.id }, migration)
    const reverted = await poll((await run(revert)).job_id, 'succeeded')
    expect(reverted.job.progress).toMatchObject({ batches_committed: 2, batches_reverted: 2, rows_reverted: 137 })
    expect(reverted.job.execution_id).not.toBe(partial.execution_id)
    expect((await history()).items[0]).toMatchObject({ id: reverted.job.execution_id, operation: 'revert_batches', direction: 'down', reverts_execution_id: null })
    expect(await position()).toMatchObject({ blocked_by_job_id: null, active_execution_id: w.execution(5) })
  })
})
