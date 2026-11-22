import { http } from 'msw/http'
import { authorizedConnection, requireWorkflow } from '../access.ts'
import { definitionChecksum } from '../checksum.ts'
import { normalizeDefinition, readDefinition, validateDefinition } from '../definition.ts'
import { ApiFailure, apiPrefix, badRequest, booleanField, conflict, forbidden, isUuid, notFound, numberField, ok, pageOf, pagingKeys, readJson, readPaging, route, secured, sortBy, stringField, type Context } from '../http.ts'
import { toMigrationSummary } from '../models.ts'
import { buildPlan, canExplain, isReadOnlyQuery } from '../plan.ts'
import { diffSchemas } from '../schema-diff.ts'
import { splitStatements } from '../sql.ts'
import { accessFor, getState, now, type MockConnection, type MockMigration } from '../state.ts'
import { Fields } from '../validation.ts'

const definitionKeys = ['title', 'up_sql', 'down_sql', 'irreversible', 'irreversible_reason', 'mode', 'batch']
const frozenMessage = 'Definisi sudah dibekukan atau pernah dieksekusi, buat draft baru'

function findMigration(ctx: Context, connection: MockConnection): MockMigration {
  const migration = getState().migrations.find((candidate) => candidate.id === ctx.params.migration_id && candidate.database_id === connection.id)
  if (!migration) throw notFound()
  return migration
}

export const migrationHandlers = [
  http.get(
    route('/databases/:database_id/migrations'),
    secured((ctx) => {
      const connection = authorizedConnection(ctx)
      requireWorkflow(connection)
      const migrations = getState()
        .migrations.filter((migration) => migration.database_id === connection.id)
        .sort(sortBy<MockMigration>((migration) => migration.created_at, (migration) => migration.id))
        .reverse()
      return ok(pageOf(migrations.map(toMigrationSummary), readPaging(ctx.url)))
    }, { query: pagingKeys }),
  ),

  http.post(
    route('/databases/:database_id/migrations'),
    secured(async (ctx) => {
      const connection = authorizedConnection(ctx, 'edit')
      requireWorkflow(connection)
      const definition = normalizeDefinition(readDefinition(await readJson(ctx.request, definitionKeys)))
      const fields = new Fields()
      validateDefinition(definition, fields)
      fields.check()
      const stamp = now()
      const migration: MockMigration = {
        ...definition,
        id: crypto.randomUUID(),
        database_id: connection.id,
        state: 'draft',
        revision: 1,
        checksum: definitionChecksum(definition),
        created_by: ctx.user.id,
        created_at: stamp,
        updated_at: stamp,
        first_execution_at: null,
      }
      getState().migrations.push(migration)
      return ok(migration, 201, { Location: `${apiPrefix}/databases/${connection.id}/migrations/${migration.id}` })
    }),
  ),

  http.get(
    route('/databases/:database_id/migrations/:migration_id'),
    secured((ctx) => {
      const connection = authorizedConnection(ctx)
      requireWorkflow(connection)
      return ok(findMigration(ctx, connection))
    }),
  ),

  // Penggantian penuh dengan CAS revision dan hanya berlaku untuk draft yang belum pernah dieksekusi
  http.patch(
    route('/databases/:database_id/migrations/:migration_id'),
    secured(async (ctx) => {
      const connection = authorizedConnection(ctx, 'edit')
      requireWorkflow(connection)
      const body = await readJson(ctx.request, [...definitionKeys, 'revision'])
      const revision = numberField(body, 'revision')
      const definition = normalizeDefinition(readDefinition(body))
      const migration = findMigration(ctx, connection)
      if (migration.state !== 'draft' || migration.first_execution_at !== null) throw conflict(frozenMessage, 'definition_frozen')
      if (revision !== undefined && revision !== migration.revision) throw conflict('Revision migration berubah, muat ulang sebelum menyimpan', 'revision_mismatch')
      const fields = new Fields()
      fields.add('revision', revision === undefined ? 'Revision wajib diisi' : null)
      validateDefinition(definition, fields)
      fields.check()
      Object.assign(migration, definition, { revision: migration.revision + 1, checksum: definitionChecksum(definition), updated_at: now() })
      return ok(migration)
    }),
  ),

  // Penjelasan rencana hanya untuk satu statement tersimpan pada mode normal dan tidak pernah mengeksekusi perubahan
  http.post(
    route('/databases/:database_id/migrations/:migration_id/plan'),
    secured(async (ctx) => {
      const connection = authorizedConnection(ctx, 'edit')
      requireWorkflow(connection)
      const body = await readJson(ctx.request, ['revision', 'direction', 'statement', 'analyze'])
      const revision = numberField(body, 'revision')
      const direction = stringField(body, 'direction')
      const statementNumber = numberField(body, 'statement')
      const analyze = booleanField(body, 'analyze')
      const migration = findMigration(ctx, connection)
      if (revision !== undefined && revision !== migration.revision) throw conflict('Revision migration berubah, muat ulang sebelum menjelaskan', 'revision_mismatch')
      const fields = new Fields()
      fields.add('revision', revision === undefined ? 'Revision wajib diisi' : null)
      fields.add('direction', direction === 'up' || direction === 'down' ? null : 'Arah harus up atau down')
      fields.add('analyze', analyze === undefined ? 'Analyze wajib diisi' : null)
      const sql = direction === 'down' ? migration.down_sql : migration.up_sql
      const statements = sql === null ? [] : splitStatements(sql)
      const index = statementNumber ?? 0
      if (direction === 'down' && migration.irreversible) fields.add('direction', 'Migration irreversible tidak memiliki SQL down')
      if (migration.mode === 'batch') fields.add('statement', 'Penjelasan hanya tersedia untuk migration mode normal')
      else if (!Number.isInteger(index) || index < 1 || index > statements.length) fields.add('statement', 'Nomor statement di luar jangkauan')
      else if (!canExplain(statements[index - 1])) fields.add('statement', 'Jenis statement ini tidak dapat dijelaskan')
      else if (analyze && !isReadOnlyQuery(statements[index - 1])) fields.add('analyze', 'ANALYZE hanya untuk SELECT yang tidak mengubah data')
      fields.check()
      const { plan, planningMs, executionMs } = buildPlan(statements[index - 1], connection.id, analyze === true)
      return ok({ revision: migration.revision, checksum: migration.checksum, direction, statement: index, analyze: analyze === true, plan, planning_ms: planningMs, execution_ms: executionMs })
    }),
  ),

  // Kedua database harus bisa dibaca pemanggil dan hasilnya hanya SQL kandidat yang tidak disimpan atau dijalankan
  http.get(
    route('/databases/:database_id/schema-diff'),
    secured((ctx) => {
      const current = authorizedConnection(ctx)
      const query = ctx.url.searchParams
      const desiredId = query.get('desired_database_id')
      if (!desiredId || !isUuid(desiredId)) throw badRequest()
      const desired = getState().connections.find((candidate) => candidate.id === desiredId)
      if (!desired || !accessFor(ctx.user.id, desired.id)) throw forbidden()
      for (const connection of [current, desired]) {
        if (connection.engine !== 'postgresql') throw new ApiFailure(422, 'validation_failed', 'Perbandingan schema hanya tersedia untuk database PostgreSQL')
      }
      const currentSchema = query.get('current_schema') ?? 'public'
      const desiredSchema = query.get('desired_schema') ?? 'public'
      const state = getState()
      const currentTables = state.tables.filter((table) => table.database_id === current.id)
      const desiredTables = state.tables.filter((table) => table.database_id === desired.id)
      const fields = new Fields()
      fields.add('current_schema', currentTables.some((table) => table.schema === currentSchema) ? null : 'Schema tidak ditemukan')
      fields.add('desired_schema', desiredTables.some((table) => table.schema === desiredSchema) ? null : 'Schema tidak ditemukan')
      fields.check()
      return ok({ current_database_id: current.id, desired_database_id: desired.id, current_schema: currentSchema, desired_schema: desiredSchema, ...diffSchemas(currentTables, desiredTables, currentSchema, desiredSchema) })
    }, { query: ['desired_database_id', 'current_schema', 'desired_schema'] }),
  ),
]
