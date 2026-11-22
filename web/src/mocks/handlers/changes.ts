import { http } from 'msw/http'
import type { Schemas } from '../../api/types.ts'
import { authorizedConnection, requireWorkflow } from '../access.ts'
import { validateDefinition } from '../definition.ts'
import { ApiFailure, apiPrefix, badRequest, conflict, forbidden, isUuid, notFound, numberField, ok, pageOf, pagingKeys, readJson, readPaging, route, secured, sortBy, stringField, type Context } from '../http.ts'
import { createJob } from '../jobs.ts'
import { blockingJob, computePosition, databaseExecutions, walkHistory } from '../position.ts'
import { getState, now, type MockChangeRequest, type MockConnection, type MockMigration } from '../state.ts'
import { Fields } from '../validation.ts'

const kinds: Schemas['ChangeKind'][] = ['apply', 'rollback', 'forward', 'resume', 'revert_batches']
const statuses: Schemas['CRStatus'][] = ['pending_review', 'approved', 'rejected', 'queued', 'running', 'succeeded', 'failed', 'partial', 'uncertain', 'partially_reverted']
const openStatuses = new Set<Schemas['CRStatus']>(['pending_review', 'approved', 'queued', 'running'])
const recoverable = new Set<Schemas['JobStatus']>(['partial', 'partially_reverted'])
const maxNoteLength = 1000

function findRequest(ctx: Context, connection: MockConnection): MockChangeRequest {
  const request = getState().changeRequests.find((candidate) => candidate.id === ctx.params.change_request_id && candidate.database_id === connection.id)
  if (!request) throw notFound()
  return request
}

const touch = (request: MockChangeRequest, status: Schemas['CRStatus']) => {
  request.status = status
  request.revision += 1
  request.updated_at = now()
}

// Approver adalah leader setiap group yang punya akses ke database dan group tanpa leader aktif menggagalkan pengajuan
function snapshotApprovals(databaseId: string): Schemas['Approval'][] {
  const state = getState()
  const approvals = state.grants
    .filter((grant) => grant.database_id === databaseId)
    .map((grant): Schemas['Approval'] => {
      const group = state.groups.find((candidate) => candidate.id === grant.group_id)
      const leader = group?.leader_user_id ? state.users.find((candidate) => candidate.id === group.leader_user_id) : undefined
      if (!group || !leader || !leader.active || !group.members.includes(leader.id)) {
        throw new ApiFailure(422, 'validation_failed', `Group ${group?.name ?? ''} belum memiliki leader aktif sehingga permintaan tidak dapat diajukan`, { reason: 'missing_leader' })
      }
      return { group_id: group.id, group_name: group.name, access: grant.access, leader_user_id: leader.id, decision: null, actor_id: null, note: '', decided_at: null }
    })
    .sort(sortBy<Schemas['Approval']>((approval) => approval.group_id))
  if (approvals.length === 0) throw new ApiFailure(422, 'validation_failed', 'Database belum memiliki group yang berhak melakukan approval', { reason: 'missing_leader' })
  return approvals
}

interface Submission {
  migration: MockMigration
  kind: Schemas['ChangeKind']
  target: string | null
  source: string | null
}

// Aturan tiap jenis permintaan: posisi aktif untuk rollback dan forward, serta job sumber yang menahan database untuk resume dan revert
function checkSubmission(connection: MockConnection, { migration, kind, target, source }: Submission) {
  const state = getState()
  const position = computePosition(connection.id)
  const blocker = blockingJob(connection.id)
  if (kind === 'apply') {
    if (migration.state !== 'draft' || migration.first_execution_at !== null) throw conflict('Migration ini sudah dibekukan atau pernah dieksekusi, buat draft baru', 'definition_frozen')
    if (walkHistory(databaseExecutions(connection.id)).redo.length > 0) throw conflict('Masih ada migration yang di-rollback, jalankan forward atau rollback sampai posisi baru terbentuk', 'position_changed')
    if (blocker && !(source === blocker.id && recoverable.has(blocker.status))) throw conflict('Database ditahan oleh job yang belum selesai dipulihkan', 'database_blocked')
    const fields = new Fields()
    validateDefinition(migration, fields)
    fields.check()
    return
  }
  if (kind === 'rollback' || kind === 'forward') {
    if (blocker) throw conflict('Database ditahan oleh job yang belum selesai dipulihkan', 'database_blocked')
    const execution = state.executions.find((candidate) => candidate.id === target && candidate.database_id === connection.id)
    if (!execution) throw notFound()
    if (execution.migration_id !== migration.id) throw new ApiFailure(422, 'validation_failed', 'Input tidak valid', { fields: { migration_id: ['Migration tidak sesuai dengan eksekusi target'] } })
    if (kind === 'rollback' && migration.irreversible) throw conflict('Migration ini ditandai tidak dapat dibatalkan', 'irreversible')
    const expected = kind === 'rollback' ? position.rollback_execution_id : position.forward_execution_id
    if (expected !== execution.id) throw conflict('Posisi migration sudah berubah, muat ulang riwayat lalu ajukan ulang', 'position_changed')
    return
  }
  const job = state.jobs.find((candidate) => candidate.id === source && candidate.database_id === connection.id)
  if (!job) throw notFound()
  if (job.migration_id !== migration.id) throw new ApiFailure(422, 'validation_failed', 'Input tidak valid', { fields: { migration_id: ['Migration tidak sesuai dengan job sumber'] } })
  if (job.status === 'uncertain') throw conflict('Hasil job belum pasti, tunggu rekonsiliasi terlebih dahulu')
  if (job.resolved || !recoverable.has(job.status)) throw conflict('Job sumber tidak memerlukan pemulihan')
  if (blocker?.id !== job.id) throw conflict('Posisi database sudah berubah, muat ulang lalu ajukan ulang', 'position_changed')
}

export const changeHandlers = [
  http.get(
    route('/databases/:database_id/change-requests'),
    secured((ctx) => {
      const connection = authorizedConnection(ctx)
      requireWorkflow(connection)
      const status = ctx.url.searchParams.get('status')
      if (status !== null && !statuses.includes(status as Schemas['CRStatus'])) throw badRequest()
      const requests = getState()
        .changeRequests.filter((request) => request.database_id === connection.id && (status === null || request.status === status))
        .sort(sortBy<MockChangeRequest>((request) => request.created_at, (request) => request.id))
        .reverse()
      return ok(pageOf(requests, readPaging(ctx.url, ['status'])))
    }, { query: [...pagingKeys, 'status'] }),
  ),

  http.post(
    route('/databases/:database_id/change-requests'),
    secured(async (ctx) => {
      const connection = authorizedConnection(ctx, 'edit')
      requireWorkflow(connection)
      const body = await readJson(ctx.request, ['migration_id', 'migration_revision', 'kind', 'target_execution_id', 'source_job_id'])
      const migrationId = stringField(body, 'migration_id')
      const revision = numberField(body, 'migration_revision')
      const kind = stringField(body, 'kind') as Schemas['ChangeKind'] | undefined
      const target = stringField(body, 'target_execution_id') ?? null
      const source = stringField(body, 'source_job_id') ?? null
      const fields = new Fields()
      fields.add('migration_id', migrationId !== undefined && isUuid(migrationId) ? null : 'Migration wajib dipilih')
      fields.add('migration_revision', revision === undefined ? 'Revision migration wajib diisi' : null)
      fields.add('kind', kind !== undefined && kinds.includes(kind) ? null : 'Jenis permintaan tidak dikenal')
      if (kind === 'apply') fields.add('target_execution_id', target === null ? null : 'Apply tidak memakai eksekusi target')
      if (kind === 'rollback' || kind === 'forward') fields.add('target_execution_id', target !== null && isUuid(target) ? null : 'Eksekusi target wajib diisi')
      if (kind === 'resume' || kind === 'revert_batches') fields.add('source_job_id', source !== null && isUuid(source) ? null : 'Job sumber wajib diisi')
      fields.check()
      const state = getState()
      const migration = state.migrations.find((candidate) => candidate.id === migrationId && candidate.database_id === connection.id)
      if (!migration) throw notFound()
      if (revision !== migration.revision) throw conflict('Revision migration berubah, muat ulang sebelum mengajukan', 'revision_mismatch')
      if (state.changeRequests.some((request) => request.database_id === connection.id && openStatuses.has(request.status))) throw conflict('Masih ada permintaan perubahan yang berjalan untuk database ini')
      checkSubmission(connection, { migration, kind: kind as Schemas['ChangeKind'], target, source })
      const approvals = snapshotApprovals(connection.id)
      const stamp = now()
      const request: MockChangeRequest = {
        id: crypto.randomUUID(),
        database_id: connection.id,
        migration_id: migration.id,
        migration_revision: migration.revision,
        checksum: migration.checksum,
        revision: 1,
        kind: kind as Schemas['ChangeKind'],
        status: 'pending_review',
        requested_by: ctx.user.id,
        target_execution_id: target,
        source_job_id: source,
        expected_position_version: computePosition(connection.id).version,
        approvals,
        created_at: stamp,
        updated_at: stamp,
      }
      state.changeRequests.push(request)
      if (kind === 'apply') {
        migration.state = 'frozen'
        migration.updated_at = stamp
      }
      return ok(request, 201, { Location: `${apiPrefix}/databases/${connection.id}/change-requests/${request.id}` })
    }),
  ),

  http.get(
    route('/databases/:database_id/change-requests/:change_request_id'),
    secured((ctx) => {
      const connection = authorizedConnection(ctx)
      requireWorkflow(connection)
      return ok(findRequest(ctx, connection))
    }),
  ),

  // Satu keputusan per group dari leader yang ada di snapshot, keputusan identik diulang tetap sukses dan yang berbeda adalah konflik
  http.post(
    route('/databases/:database_id/change-requests/:change_request_id/approvals'),
    secured(async (ctx) => {
      const connection = authorizedConnection(ctx)
      requireWorkflow(connection)
      const body = await readJson(ctx.request, ['revision', 'group_id', 'decision', 'note'])
      const revision = numberField(body, 'revision')
      const groupId = stringField(body, 'group_id')
      const decision = stringField(body, 'decision')
      const note = (stringField(body, 'note') ?? '').trim()
      const request = findRequest(ctx, connection)
      const fields = new Fields()
      fields.add('revision', revision === undefined ? 'Revision wajib diisi' : null)
      fields.add('group_id', groupId !== undefined && isUuid(groupId) ? null : 'Group wajib dipilih')
      fields.add('decision', decision === 'approve' || decision === 'reject' ? null : 'Keputusan harus approve atau reject')
      fields.add('note', Array.from(note).length > maxNoteLength ? `Catatan maksimal ${maxNoteLength} karakter` : null)
      fields.check()
      const approval = request.approvals.find((candidate) => candidate.group_id === groupId)
      if (!approval) throw new ApiFailure(422, 'validation_failed', 'Input tidak valid', { fields: { group_id: ['Group tidak termasuk approver permintaan ini'] } })
      if (approval.leader_user_id !== ctx.user.id) throw forbidden()
      if (approval.decision !== null) {
        if (approval.decision === decision && approval.actor_id === ctx.user.id && approval.note === note) return ok(request)
        throw conflict('Keputusan untuk group ini sudah tercatat dan tidak dapat diubah')
      }
      if (request.requested_by === ctx.user.id) throw new ApiFailure(403, 'forbidden', 'Pengaju tidak dapat menyetujui permintaannya sendiri', { reason: 'self_approval' })
      if (getState().groups.find((group) => group.id === approval.group_id)?.leader_user_id !== ctx.user.id) throw conflict('Leader group berubah sejak diajukan, ajukan ulang permintaan', 'stale_approval')
      if (request.status !== 'pending_review') throw conflict('Permintaan sudah tidak menunggu review')
      if (revision !== request.revision) throw conflict('Permintaan berubah, muat ulang sebelum memutuskan', 'revision_mismatch')
      approval.decision = decision as 'approve' | 'reject'
      approval.actor_id = ctx.user.id
      approval.note = note
      approval.decided_at = now()
      if (decision === 'reject') touch(request, 'rejected')
      else if (request.approvals.every((candidate) => candidate.decision === 'approve')) touch(request, 'approved')
      else touch(request, request.status)
      return ok(request)
    }),
  ),

  // Mengantrekan tidak menunggu SQL target selesai dan pengulangan yang sah mengembalikan job yang sama dengan status terkininya
  http.post(
    route('/databases/:database_id/change-requests/:change_request_id/execute'),
    secured(async (ctx) => {
      const connection = authorizedConnection(ctx, 'edit')
      requireWorkflow(connection)
      const body = await readJson(ctx.request, ['revision'])
      const revision = numberField(body, 'revision')
      const request = findRequest(ctx, connection)
      const state = getState()
      const existing = state.jobs.find((job) => job.change_request_id === request.id)
      if (existing) return ok({ job_id: existing.id, status: existing.status }, 202, { Location: `${apiPrefix}/jobs/${existing.id}` })
      const fields = new Fields()
      fields.add('revision', revision === undefined ? 'Revision wajib diisi' : null)
      fields.check()
      if (request.status !== 'approved') throw conflict('Permintaan belum disetujui atau sudah tidak dapat dijalankan')
      if (revision !== request.revision) throw conflict('Permintaan berubah, muat ulang sebelum menjalankan', 'revision_mismatch')
      if (computePosition(connection.id).version !== request.expected_position_version) throw conflict('Posisi database berubah sejak pengajuan, ajukan ulang permintaan', 'position_changed')
      const migration = state.migrations.find((candidate) => candidate.id === request.migration_id)
      if (!migration) throw notFound()
      const job = createJob(request, migration)
      touch(request, 'queued')
      return ok({ job_id: job.id, status: job.status }, 202, { Location: `${apiPrefix}/jobs/${job.id}` })
    }),
  ),
]
