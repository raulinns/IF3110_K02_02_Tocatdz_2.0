import { describe, expect, it } from 'vitest'
import type { Schemas } from '../../api/types.ts'
import { definitionChecksum, type Definition } from '../checksum.ts'
import { ids, workflowIds as w } from '../ids.ts'
import { createJob } from '../jobs.ts'
import { getState, now, type MockMigration } from '../state.ts'
import { failureOf, newClient, setupMockServer, signIn } from '../testing.ts'
import { authHandlers } from './auth.ts'
import { jobHandlers } from './jobs.ts'

setupMockServer(...authHandlers, ...jobHandlers)

type Page<T> = { items: T[]; page: number; limit: number; has_next: boolean }

function queuedJob(sql = 'SELECT 1;') {
  const definition: Definition = { title: 'Uji job', up_sql: sql, down_sql: 'SELECT 2;', irreversible: false, irreversible_reason: '', mode: 'normal', batch: null }
  const migration: MockMigration = { ...definition, id: crypto.randomUUID(), database_id: ids.inventaris, state: 'frozen', revision: 1, checksum: definitionChecksum(definition), created_by: ids.editor, created_at: now(), updated_at: now(), first_execution_at: null }
  getState().migrations.push(migration)
  const request: Schemas['ChangeRequest'] = { id: crypto.randomUUID(), database_id: ids.inventaris, migration_id: migration.id, migration_revision: 1, checksum: migration.checksum, revision: 3, kind: 'apply', status: 'approved', requested_by: ids.editor, target_execution_id: null, source_job_id: null, expected_position_version: 5, approvals: [], created_at: now(), updated_at: now() }
  getState().changeRequests.push(request)
  return createJob(request, migration)
}

describe('GET /jobs/{id}', () => {
  it('membaca job yang sudah selesai tanpa field internal simulasi', async () => {
    const { client } = await signIn('reviewer')
    const job = await client.get<Schemas['Job']>(`/jobs/${w.job(5)}`)
    expect(job).toEqual({
      id: w.job(5),
      database_id: ids.inventaris,
      change_request_id: w.changeRequest(5),
      execution_id: w.execution(5),
      migration_id: w.migrationC,
      kind: 'apply',
      mode: 'batch',
      status: 'succeeded',
      attempt: 1,
      max_attempts: 3,
      run_after: expect.any(String),
      queued_at: expect.any(String),
      started_at: '2026-11-10T10:05:00Z',
      finished_at: '2026-11-10T10:05:01Z',
      partial: false,
      progress: { batches_committed: 2, rows_committed: 137, batches_reverted: 0, rows_reverted: 0, total_batches: 2 },
      error: null,
    })
    await expect(client.get(`/jobs/${w.job(5)}`)).resolves.toMatchObject({ status: 'succeeded' })
  })

  it('setiap polling mengirim status saat ini lalu memajukan job satu langkah', async () => {
    const job = queuedJob()
    const { client } = await signIn('reviewer')
    const seen: string[] = []
    for (let i = 0; i < 4; i++) seen.push((await client.get<Schemas['Job']>(`/jobs/${job.id}`)).status)
    expect(seen).toEqual(['queued', 'running', 'succeeded', 'succeeded'])
  })

  it('404 untuk job yang tidak ada atau milik database tanpa akses, 400 untuk ID yang salah, 401 tanpa sesi', async () => {
    expect((await failureOf(newClient().get(`/jobs/${w.job(1)}`, { handleUnauthenticated: false }))).status).toBe(401)
    const auditor = await signIn('auditor')
    expect((await failureOf(auditor.client.get(`/jobs/${w.job(1)}`))).code).toBe('not_found')
    const reviewer = await signIn('reviewer')
    expect((await failureOf(reviewer.client.get('/jobs/60000000-0000-4000-8000-000000000099'))).code).toBe('not_found')
    expect((await failureOf(reviewer.client.get('/jobs/bukan-uuid'))).code).toBe('bad_request')
    expect((await failureOf(reviewer.client.get(`/jobs/${w.job(1)}`, { query: { x: 1 } }))).code).toBe('bad_request')
  })

  it('view_only boleh memantau job', async () => {
    const { client } = await signIn('observer')
    await expect(client.get(`/jobs/${w.job(1)}`)).resolves.toMatchObject({ id: w.job(1) })
  })
})

describe('GET /jobs/{id}/attempts', () => {
  it('memuat semua attempt termasuk yang gagal, terurut menurut attempt', async () => {
    const job = queuedJob('-- mock:retry\nSELECT 1;')
    const { client } = await signIn('reviewer')
    for (let i = 0; i < 5; i++) await client.get(`/jobs/${job.id}`)
    const page = await client.get<Page<Schemas['JobAttempt']>>(`/jobs/${job.id}/attempts`)
    expect(page.items.map((attempt) => [attempt.attempt, attempt.status])).toEqual([
      [1, 'failed'],
      [2, 'succeeded'],
    ])
    expect(page.items[0].error).toEqual({ code: 'connection_error', message: expect.any(String), sqlstate: '08006', retryable: true })
    expect(page.items[1].error).toBeNull()
    const first = await client.get<Page<Schemas['JobAttempt']>>(`/jobs/${job.id}/attempts`, { query: { limit: 1 } })
    expect(first.has_next).toBe(true)
  })

  it('job yang belum pernah dijalankan belum punya attempt dan akses dibatasi seperti job', async () => {
    const job = queuedJob()
    const { client } = await signIn('reviewer')
    await expect(client.get(`/jobs/${job.id}/attempts`)).resolves.toEqual({ items: [], page: 1, limit: 50, has_next: false })
    const auditor = await signIn('auditor')
    expect((await failureOf(auditor.client.get(`/jobs/${job.id}/attempts`))).code).toBe('not_found')
  })
})

describe('GET /databases/{id}/jobs', () => {
  const path = `/databases/${ids.inventaris}/jobs`

  it('daftar job terbaru lebih dulu dan dapat disaring menurut status', async () => {
    const { client } = await signIn('reviewer')
    const all = await client.get<Page<Schemas['Job']>>(path)
    expect(all.items.map((job) => job.id)).toEqual([5, 4, 3, 2, 1].map(w.job))
    expect((await client.get<Page<Schemas['Job']>>(path, { query: { status: 'succeeded', limit: 2 } })).items).toHaveLength(2)
    expect((await client.get<Page<Schemas['Job']>>(path, { query: { status: 'running' } })).items).toEqual([])
    queuedJob()
    expect((await client.get<Page<Schemas['Job']>>(path, { query: { status: 'queued' } })).items).toHaveLength(1)
  })

  it('menolak status atau query yang tidak dikenal, tanpa akses, dan database MySQL', async () => {
    const { client } = await signIn('observer')
    expect((await failureOf(client.get(path, { query: { status: 'selesai' } }))).code).toBe('bad_request')
    expect((await failureOf(client.get(path, { query: { q: 'x' } }))).code).toBe('bad_request')
    expect((await failureOf(client.get(`/databases/${ids.arsip}/jobs`))).status).toBe(422)
    const auditor = await signIn('auditor')
    expect((await failureOf(auditor.client.get(path))).code).toBe('forbidden')
  })
})
