import { ids, workflowIds } from '../mocks/ids.ts'
import { createJob } from '../mocks/jobs.ts'
import { computePosition } from '../mocks/position.ts'
import { getState, type MockChangeRequest, type MockJob, type MockMigration } from '../mocks/state.ts'

const stamp = '2026-11-12T09:00:00Z'

// Permintaan perubahan untuk migration D dengan dua approver, ditambahkan langsung ke state mock supaya tiap tahap review bisa diuji
export function seedRequest(patch: Partial<MockChangeRequest> = {}): MockChangeRequest {
  const state = getState()
  const migrationId = patch.migration_id ?? workflowIds.migrationD
  const migration = state.migrations.find((candidate) => candidate.id === migrationId)!
  const request: MockChangeRequest = {
    id: crypto.randomUUID(),
    database_id: ids.inventaris,
    migration_id: migrationId,
    migration_revision: migration.revision,
    checksum: migration.checksum,
    revision: 1,
    kind: 'apply',
    status: 'pending_review',
    requested_by: ids.editor,
    target_execution_id: null,
    source_job_id: null,
    expected_position_version: computePosition(ids.inventaris).version,
    approvals: [
      { group_id: ids.writers, group_name: 'writers', access: 'view_edit', leader_user_id: ids.reviewer, decision: null, actor_id: null, note: '', decided_at: null },
      { group_id: ids.readers, group_name: 'readers', access: 'view_only', leader_user_id: ids.observer, decision: null, actor_id: null, note: '', decided_at: null },
    ],
    created_at: stamp,
    updated_at: stamp,
    ...patch,
  }
  migration.state = 'frozen'
  state.changeRequests.push(request)
  return request
}

// Menyetujui semua group sekaligus untuk permintaan yang langsung diuji pada tahap siap dijalankan
export function approveAll(request: MockChangeRequest) {
  for (const approval of request.approvals) Object.assign(approval, { decision: 'approve', actor_id: approval.leader_user_id, decided_at: stamp })
  request.status = 'approved'
  request.revision += 1
}

// Migration batch baru yang SQL-nya bisa diberi komentar -- mock:partial atau -- mock:uncertain untuk memilih hasil job
export function seedBatchMigration(up = "status = 'arsip'"): MockMigration {
  const base = getState().migrations.find((candidate) => candidate.id === workflowIds.migrationC)!
  const migration: MockMigration = { ...base, id: crypto.randomUUID(), title: 'Batch uji', up_sql: up, state: 'frozen', first_execution_at: null, revision: 1 }
  getState().migrations.push(migration)
  return migration
}

// Job baru dalam keadaan antre untuk permintaan yang sudah disetujui, lalu worker mock bergerak setiap job dibaca
export function seedJob(migration?: MockMigration, kind: MockChangeRequest['kind'] = 'apply'): { request: MockChangeRequest; job: MockJob } {
  const target = migration ?? getState().migrations.find((candidate) => candidate.id === workflowIds.migrationD)!
  const request = seedRequest({ migration_id: target.id, kind })
  approveAll(request)
  const job = createJob(request, target)
  request.status = 'queued'
  return { request, job }
}

