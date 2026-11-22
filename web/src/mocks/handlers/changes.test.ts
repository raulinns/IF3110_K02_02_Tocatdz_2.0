import { describe, expect, it } from 'vitest'
import type { Schemas } from '../../api/types.ts'
import { definitionChecksum, type Definition } from '../checksum.ts'
import { ids, workflowIds as w } from '../ids.ts'
import { createJob, tickJob } from '../jobs.ts'
import { bumpPosition } from '../position.ts'
import { getState, now, type MockJob, type MockMigration } from '../state.ts'
import { failureOf, setupMockServer, signIn } from '../testing.ts'
import { authHandlers } from './auth.ts'
import { changeHandlers } from './changes.ts'
import { migrationHandlers } from './migrations.ts'

setupMockServer(...authHandlers, ...migrationHandlers, ...changeHandlers)

type Page<T> = { items: T[]; page: number; limit: number; has_next: boolean }
type Request = Schemas['ChangeRequest']

const crs = `/databases/${ids.inventaris}/change-requests`
const act = async (username: string) => (await signIn(username)).client

const submit = async (user: string, migrationId: string, extra: Record<string, unknown> = {}) => (await act(user)).post<Request>(crs, { migration_id: migrationId, migration_revision: 1, kind: 'apply', ...extra })
const decide = async (user: string, request: Request, groupId: string, decision: string, extra: Record<string, unknown> = {}) =>
  (await act(user)).post<Request>(`${crs}/${request.id}/approvals`, { revision: request.revision, group_id: groupId, decision, ...extra })
const execute = async (user: string, request: Request, body: Record<string, unknown> = { revision: request.revision }) => (await act(user)).post<Schemas['JobAccepted']>(`${crs}/${request.id}/execute`, body)

async function approvedRequest(migrationId: string = w.migrationD): Promise<Request> {
  const pending = await submit('editor', migrationId)
  const half = await decide('reviewer', pending, ids.writers, 'approve')
  return decide('observer', half, ids.readers, 'approve')
}

function partialJob(): { migration: MockMigration; job: MockJob } {
  const definition: Definition = { title: 'Batch partial', up_sql: "status = 'x' -- mock:partial", down_sql: "status = 'y'", irreversible: false, irreversible_reason: '', mode: 'batch', batch: { schema: 'public', table: 'orders', key_column: 'id', batch_size: 100, scope_where: null } }
  const migration: MockMigration = { ...definition, id: crypto.randomUUID(), database_id: ids.inventaris, state: 'frozen', revision: 1, checksum: definitionChecksum(definition), created_by: ids.editor, created_at: now(), updated_at: now(), first_execution_at: null }
  getState().migrations.push(migration)
  const request: Request = { id: crypto.randomUUID(), database_id: ids.inventaris, migration_id: migration.id, migration_revision: 1, checksum: migration.checksum, revision: 3, kind: 'apply', status: 'approved', requested_by: ids.editor, target_execution_id: null, source_job_id: null, expected_position_version: 5, approvals: [], created_at: now(), updated_at: now() }
  getState().changeRequests.push(request)
  const job = createJob(request, migration)
  for (let i = 0; i < 4; i++) tickJob(job)
  return { migration, job }
}

describe('GET change-requests', () => {
  it('daftar terbaru lebih dulu dengan approver per group', async () => {
    const client = await act('reviewer')
    const page = await client.get<Page<Request>>(crs)
    expect(page.items.map((item) => item.id)).toEqual([5, 4, 3, 2, 1].map(w.changeRequest))
    expect(page.items[4]).toMatchObject({ kind: 'apply', status: 'succeeded', requested_by: ids.editor })
    expect(page.items[0].approvals).toEqual([
      { group_id: ids.writers, group_name: 'writers', access: 'view_edit', leader_user_id: ids.reviewer, decision: 'approve', actor_id: ids.reviewer, note: '', decided_at: expect.any(String) },
      { group_id: ids.readers, group_name: 'readers', access: 'view_only', leader_user_id: ids.observer, decision: 'approve', actor_id: ids.observer, note: '', decided_at: expect.any(String) },
    ])
  })

  it('menyaring menurut status, memaginasi, dan menolak status atau query yang tidak dikenal', async () => {
    const client = await act('observer')
    expect((await client.get<Page<Request>>(crs, { query: { status: 'succeeded' } })).items).toHaveLength(5)
    expect((await client.get<Page<Request>>(crs, { query: { status: 'pending_review' } })).items).toEqual([])
    const page = await client.get<Page<Request>>(crs, { query: { limit: 2 } })
    expect(page.items).toHaveLength(2)
    expect(page.has_next).toBe(true)
    expect((await failureOf(client.get(crs, { query: { status: 'selesai' } }))).code).toBe('bad_request')
    expect((await failureOf(client.get(crs, { query: { q: 'x' } }))).code).toBe('bad_request')
  })

  it('detail permintaan, 404 untuk yang tidak ada, 403 tanpa akses, dan 422 untuk MySQL', async () => {
    const client = await act('observer')
    await expect(client.get(`${crs}/${w.changeRequest(3)}`)).resolves.toMatchObject({ id: w.changeRequest(3), kind: 'rollback', target_execution_id: w.execution(2) })
    expect((await failureOf(client.get(`${crs}/50000000-0000-4000-8000-000000000099`))).code).toBe('not_found')
    expect((await failureOf((await act('auditor')).get(crs))).code).toBe('forbidden')
    expect((await failureOf((await act('observer')).get(`/databases/${ids.arsip}/change-requests`))).status).toBe(422)
  })
})

describe('POST change-requests', () => {
  it('mengajukan apply: snapshot approver per group, versi posisi, dan migration menjadi beku', async () => {
    const client = await act('editor')
    const request = await client.post<Request>(crs, { migration_id: w.migrationD, migration_revision: 1, kind: 'apply' })
    expect(request).toMatchObject({ database_id: ids.inventaris, migration_id: w.migrationD, migration_revision: 1, revision: 1, kind: 'apply', status: 'pending_review', requested_by: ids.editor, target_execution_id: null, source_job_id: null, expected_position_version: 5 })
    expect(request.checksum).toBe(getState().migrations.find((migration) => migration.id === w.migrationD)?.checksum)
    expect(request.approvals.map((approval) => [approval.group_name, approval.access, approval.leader_user_id, approval.decision])).toEqual([
      ['writers', 'view_edit', ids.reviewer, null],
      ['readers', 'view_only', ids.observer, null],
    ])
    expect((await client.get<Schemas['Migration']>(`/databases/${ids.inventaris}/migrations/${w.migrationD}`)).state).toBe('frozen')
  })

  it('view_only dan user tanpa akses tidak boleh mengajukan', async () => {
    expect((await failureOf(submit('observer', w.migrationD))).code).toBe('forbidden')
    expect((await failureOf(submit('auditor', w.migrationD))).code).toBe('forbidden')
    expect(getState().changeRequests).toHaveLength(5)
  })

  it('group tanpa leader menggagalkan pengajuan tanpa membekukan migration', async () => {
    const client = await act('editor')
    const err = await failureOf(client.post(`/databases/${ids.pelaporan}/change-requests`, { migration_id: w.migrationP, migration_revision: 1, kind: 'apply' }))
    expect(err).toMatchObject({ status: 422, code: 'validation_failed', reason: 'missing_leader' })
    expect(err.message).toContain('magang')
    expect(getState().migrations.find((migration) => migration.id === w.migrationP)?.state).toBe('draft')
  })

  it('validasi per field untuk kombinasi jenis, target, dan job sumber', async () => {
    const client = await act('editor')
    const post = (body: unknown) => failureOf(client.post(crs, body))
    expect((await post({})).fields).toMatchObject({ migration_id: expect.any(Array), migration_revision: expect.any(Array), kind: expect.any(Array) })
    expect((await post({ migration_id: w.migrationD, migration_revision: 1, kind: 'hapus' })).fields.kind).toHaveLength(1)
    expect((await post({ migration_id: w.migrationD, migration_revision: 1, kind: 'apply', target_execution_id: w.execution(1) })).fields.target_execution_id).toHaveLength(1)
    expect((await post({ migration_id: w.migrationC, migration_revision: 1, kind: 'rollback' })).fields.target_execution_id).toHaveLength(1)
    expect((await post({ migration_id: w.migrationC, migration_revision: 1, kind: 'resume' })).fields.source_job_id).toHaveLength(1)
    expect((await post({ migration_id: w.migrationD, migration_revision: 1, kind: 'apply', extra: 1 })).code).toBe('bad_request')
  })

  it('revision migration usang adalah konflik dan migration yang tidak ada 404', async () => {
    const client = await act('editor')
    expect(await failureOf(client.post(crs, { migration_id: w.migrationD, migration_revision: 7, kind: 'apply' }))).toMatchObject({ status: 409, reason: 'revision_mismatch' })
    expect((await failureOf(client.post(crs, { migration_id: '20000000-0000-4000-8000-000000000099', migration_revision: 1, kind: 'apply' }))).code).toBe('not_found')
    expect((await failureOf(client.post(crs, { migration_id: w.migrationP, migration_revision: 1, kind: 'apply' }))).code).toBe('not_found')
  })

  it('migration yang sudah dieksekusi tidak bisa diajukan apply lagi', async () => {
    expect(await failureOf(submit('editor', w.migrationA))).toMatchObject({ status: 409, reason: 'definition_frozen' })
  })

  it('hanya satu permintaan aktif per database', async () => {
    await submit('editor', w.migrationD)
    const err = await failureOf(submit('editor', w.migrationE))
    expect(err.code).toBe('conflict')
    expect(err.message).toContain('berjalan')
  })

  it('apply ditolak selama masih ada migration yang di-rollback', async () => {
    const state = getState()
    state.executions.push({ ...state.executions[4], id: w.execution(6), operation: 'rollback', direction: 'down', reverts_execution_id: w.execution(5), position_version: bumpPosition(ids.inventaris), statements: [] })
    expect(await failureOf(submit('editor', w.migrationD))).toMatchObject({ status: 409, reason: 'position_changed' })
  })

  it('rollback hanya untuk eksekusi teratas yang reversible', async () => {
    const rollback = (user: string, migrationId: string, target: string) => submit(user, migrationId, { kind: 'rollback', target_execution_id: target })
    const request = await rollback('editor', w.migrationC, w.execution(5))
    expect(request).toMatchObject({ kind: 'rollback', target_execution_id: w.execution(5), status: 'pending_review' })
    expect(getState().migrations.find((migration) => migration.id === w.migrationC)?.state).toBe('frozen')
    getState().changeRequests.pop()
    expect(await failureOf(rollback('editor', w.migrationB, w.execution(4)))).toMatchObject({ status: 409, reason: 'position_changed' })
    expect((await failureOf(rollback('editor', w.migrationC, w.execution(4)))).fields.migration_id).toHaveLength(1)
    expect((await failureOf(rollback('editor', w.migrationC, '70000000-0000-4000-8000-000000000099'))).code).toBe('not_found')
    getState().migrations.find((migration) => migration.id === w.migrationC)!.irreversible = true
    expect(await failureOf(rollback('editor', w.migrationC, w.execution(5)))).toMatchObject({ status: 409, reason: 'irreversible' })
  })

  it('forward hanya untuk migration berikutnya pada redo chain', async () => {
    const forward = (target: string) => submit('editor', w.migrationC, { kind: 'forward', target_execution_id: target })
    expect(await failureOf(forward(w.execution(5)))).toMatchObject({ status: 409, reason: 'position_changed' })
    const state = getState()
    state.executions.push({ ...state.executions[4], id: w.execution(6), operation: 'rollback', direction: 'down', reverts_execution_id: w.execution(5), position_version: bumpPosition(ids.inventaris), statements: [] })
    await expect(forward(w.execution(5))).resolves.toMatchObject({ kind: 'forward', target_execution_id: w.execution(5), expected_position_version: 6 })
  })

  it('database yang ditahan job yang belum dipulihkan menolak apply, rollback, dan forward', async () => {
    partialJob()
    expect(await failureOf(submit('editor', w.migrationD))).toMatchObject({ status: 409, reason: 'database_blocked' })
    expect(await failureOf(submit('editor', w.migrationC, { kind: 'rollback', target_execution_id: w.execution(5) }))).toMatchObject({ status: 409, reason: 'database_blocked' })
    expect(await failureOf(submit('editor', w.migrationC, { kind: 'forward', target_execution_id: w.execution(5) }))).toMatchObject({ status: 409, reason: 'database_blocked' })
  })

  it('resume dan revert batch merujuk job sumber yang menahan database', async () => {
    const { migration, job } = partialJob()
    const resume = await submit('editor', migration.id, { kind: 'resume', source_job_id: job.id })
    expect(resume).toMatchObject({ kind: 'resume', source_job_id: job.id, status: 'pending_review' })
    getState().changeRequests.pop()
    await expect(submit('editor', migration.id, { kind: 'revert_batches', source_job_id: job.id })).resolves.toMatchObject({ kind: 'revert_batches' })
  })

  it('resume ditolak untuk job yang tidak ada, sudah pulih, belum pasti, atau milik migration lain', async () => {
    const { migration, job } = partialJob()
    const resume = (source: string, migrationId: string = migration.id) => submit('editor', migrationId, { kind: 'resume', source_job_id: source })
    expect((await failureOf(resume('60000000-0000-4000-8000-000000000099'))).code).toBe('not_found')
    expect((await failureOf(resume(job.id, w.migrationC))).fields.migration_id).toHaveLength(1)
    job.status = 'uncertain'
    expect((await failureOf(resume(job.id))).message).toContain('belum pasti')
    job.status = 'partial'
    job.resolved = true
    expect((await failureOf(resume(job.id))).message).toContain('tidak memerlukan pemulihan')
  })
})

describe('POST approvals', () => {
  it('setiap leader memutuskan untuk groupnya dan semua approve membuat permintaan approved', async () => {
    const pending = await submit('editor', w.migrationD)
    const half = await decide('reviewer', pending, ids.writers, 'approve', { note: '  Aman  ' })
    expect(half).toMatchObject({ status: 'pending_review', revision: 2 })
    expect(half.approvals[0]).toMatchObject({ decision: 'approve', actor_id: ids.reviewer, note: 'Aman', decided_at: expect.any(String) })
    expect(half.approvals[1]).toMatchObject({ decision: null, actor_id: null, decided_at: null })
    const done = await decide('observer', half, ids.readers, 'approve')
    expect(done).toMatchObject({ status: 'approved', revision: 3 })
  })

  it('satu reject langsung menolak permintaan dan keputusan berikutnya konflik', async () => {
    const pending = await submit('editor', w.migrationD)
    const rejected = await decide('observer', pending, ids.readers, 'reject', { note: 'Belum siap' })
    expect(rejected).toMatchObject({ status: 'rejected', revision: 2 })
    expect((await failureOf(decide('reviewer', rejected, ids.writers, 'approve'))).code).toBe('conflict')
  })

  it('hanya leader group yang bersangkutan, bukan admin, anggota biasa, atau leader group lain', async () => {
    const pending = await submit('editor', w.migrationD)
    for (const user of ['admin', 'editor', 'auditor', 'pengamat']) expect((await failureOf(decide(user, pending, ids.writers, 'approve'))).code).toBe('forbidden')
    expect((await failureOf(decide('observer', pending, ids.writers, 'approve'))).code).toBe('forbidden')
    expect(getState().changeRequests.find((request) => request.id === pending.id)?.revision).toBe(1)
  })

  it('pengaju tidak boleh menyetujui permintaannya sendiri', async () => {
    const pending = await submit('reviewer', w.migrationD)
    const err = await failureOf(decide('reviewer', pending, ids.writers, 'approve'))
    expect(err).toMatchObject({ status: 403, code: 'forbidden', reason: 'self_approval' })
  })

  it('keputusan identik diulang tetap sukses, yang berbeda adalah konflik', async () => {
    const pending = await submit('editor', w.migrationD)
    const decided = await decide('reviewer', pending, ids.writers, 'approve', { note: 'ok' })
    await expect(decide('reviewer', pending, ids.writers, 'approve', { note: 'ok' })).resolves.toMatchObject({ revision: decided.revision, status: 'pending_review' })
    expect((await failureOf(decide('reviewer', pending, ids.writers, 'reject', { note: 'ok' }))).code).toBe('conflict')
    expect((await failureOf(decide('reviewer', pending, ids.writers, 'approve', { note: 'lain' }))).code).toBe('conflict')
  })

  it('revision usang dan leader yang sudah berganti ditolak', async () => {
    const pending = await submit('editor', w.migrationD)
    await decide('reviewer', pending, ids.writers, 'approve')
    expect(await failureOf(decide('observer', pending, ids.readers, 'approve'))).toMatchObject({ status: 409, reason: 'revision_mismatch' })
    getState().groups.find((group) => group.id === ids.readers)!.leader_user_id = ids.pengamat
    const fresh = getState().changeRequests.find((request) => request.id === pending.id)!
    expect(await failureOf(decide('observer', fresh, ids.readers, 'approve'))).toMatchObject({ status: 409, reason: 'stale_approval' })
  })

  it('validasi group, keputusan, catatan, dan field asing', async () => {
    const pending = await submit('editor', w.migrationD)
    const client = await act('reviewer')
    const path = `${crs}/${pending.id}/approvals`
    expect((await failureOf(client.post(path, {}))).fields).toMatchObject({ revision: expect.any(Array), group_id: expect.any(Array), decision: expect.any(Array) })
    expect((await failureOf(client.post(path, { revision: 1, group_id: ids.auditors, decision: 'approve' }))).fields.group_id).toHaveLength(1)
    expect((await failureOf(client.post(path, { revision: 1, group_id: ids.writers, decision: 'approve', note: 'x'.repeat(1001) }))).fields.note).toHaveLength(1)
    expect((await failureOf(client.post(path, { revision: 1, group_id: ids.writers, decision: 'approve', extra: 1 }))).code).toBe('bad_request')
  })
})

describe('POST execute', () => {
  it('mengantrekan permintaan yang disetujui dan menjawab 202 dengan Location job', async () => {
    const request = await approvedRequest()
    const accepted = await execute('editor', request)
    expect(accepted).toEqual({ job_id: expect.any(String), status: 'queued' })
    const job = getState().jobs.find((candidate) => candidate.id === accepted.job_id)
    expect(job).toMatchObject({ change_request_id: request.id, status: 'queued', attempt: 0, kind: 'apply' })
    expect(getState().changeRequests.find((candidate) => candidate.id === request.id)).toMatchObject({ status: 'queued', revision: request.revision + 1 })
  })

  it('mengirim Location ke job dan pengulangan mengembalikan job yang sama', async () => {
    const request = await approvedRequest()
    const { me } = await signIn('editor')
    const send = (revision: number) => fetch(`http://localhost/api/v1${crs}/${request.id}/execute`, { method: 'POST', headers: { 'Content-Type': 'application/json', 'X-CSRF-Token': me.csrf_token }, body: JSON.stringify({ revision }) })
    const first = await send(request.revision)
    expect(first.status).toBe(202)
    const body = (await first.json()) as Schemas['JobAccepted']
    expect(first.headers.get('Location')).toBe(`/api/v1/jobs/${body.job_id}`)
    const replay = await send(1)
    expect(replay.status).toBe(202)
    await expect(replay.json()).resolves.toEqual({ job_id: body.job_id, status: 'queued' })
    expect(getState().jobs.filter((job) => job.change_request_id === request.id)).toHaveLength(1)
  })

  it('menolak permintaan yang belum disetujui, revision usang, dan posisi yang sudah berubah', async () => {
    const pending = await submit('editor', w.migrationD)
    expect((await failureOf(execute('editor', pending))).code).toBe('conflict')
    const approved = await decide('observer', await decide('reviewer', pending, ids.writers, 'approve'), ids.readers, 'approve')
    expect(await failureOf(execute('editor', approved, { revision: approved.revision - 1 }))).toMatchObject({ status: 409, reason: 'revision_mismatch' })
    bumpPosition(ids.inventaris)
    expect(await failureOf(execute('editor', approved))).toMatchObject({ status: 409, reason: 'position_changed' })
    expect(getState().jobs.some((job) => job.change_request_id === approved.id)).toBe(false)
  })

  it('revision wajib dan hanya user view_edit yang boleh, termasuk leader view_only yang tidak boleh', async () => {
    const request = await approvedRequest()
    expect((await failureOf(execute('editor', request, {}))).fields.revision).toHaveLength(1)
    expect((await failureOf(execute('observer', request))).code).toBe('forbidden')
    expect((await failureOf(execute('auditor', request))).code).toBe('forbidden')
    await expect(execute('reviewer', request)).resolves.toMatchObject({ status: 'queued' })
  })
})
