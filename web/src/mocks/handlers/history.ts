import { http } from 'msw/http'
import { authorizedConnection, requireWorkflow } from '../access.ts'
import { badRequest, notFound, ok, pageOf, pagingKeys, readPaging, route, secured, type Context } from '../http.ts'
import { toExecution, toExecutionSummary } from '../models.ts'
import { computePosition, databaseExecutions, visibleExecutions } from '../position.ts'
import { getState, type MockConnection, type MockExecution } from '../state.ts'
import { attemptHistory } from '../attemptHistory.ts'
import { buildStatements } from '../statements.ts'

function findExecution({ params }: Context, connection: MockConnection): MockExecution {
  const execution = getState().executions.find((candidate) => candidate.id === params.execution_id && candidate.database_id === connection.id)
  if (!execution) throw notFound()
  return execution
}

export const historyHandlers = [
  http.get(route('/databases/:database_id/history'), secured((ctx) => {
    const connection = authorizedConnection(ctx)
    requireWorkflow(connection)
    const hide = ctx.url.searchParams.get('hide_rollback')
    if (hide !== null && hide !== 'true' && hide !== 'false') throw badRequest()
    const entries = attemptHistory(connection.id, hide === 'true').map((entry) => ({...entry, up_sql: null, down_sql: null, sql_snapshot: null}))
    return ok(pageOf(entries, readPaging(ctx.url, ['hide_rollback'])))
  }, {query: [...pagingKeys, 'hide_rollback']})),
  http.get(route('/databases/:database_id/history/:history_id'), secured((ctx) => {
    const connection = authorizedConnection(ctx)
    requireWorkflow(connection)
    const entry = attemptHistory(connection.id, false).find((entry) => entry.id === ctx.params.history_id)
    if (!entry) throw notFound()
    return ok(entry)
  })),
  http.get(route('/databases/:database_id/history/:history_id/statements'), secured((ctx) => {
    const connection = authorizedConnection(ctx)
    requireWorkflow(connection)
    const entry = attemptHistory(connection.id, false).find((entry) => entry.id === ctx.params.history_id)
    if (!entry) throw notFound()
    const execution = entry.committed_at ? getState().executions.find((item) => item.id === entry.execution_id) : undefined
    const definition = getState().migrations.find((item) => item.id === entry.migration_id)!
    const statements = execution?.statements ?? buildStatements(definition.mode, definition.batch, entry.sql_snapshot ?? '', [])
    return ok(pageOf(statements.map((statement, index) => ({...statement, ordinal: index + 1,
      status: entry.resolved_status === 'succeeded' ? 'succeeded' : entry.status === 'failed' ? 'failed' : 'started', sqlstate: entry.error?.sqlstate ?? null,
      started_at: entry.started_at, finished_at: entry.finished_at})), readPaging(ctx.url)))
  }, {query: pagingKeys})),
  // Riwayat terbaru lebih dulu dan Hide rollback memasangkan eksekusi dengan rollback-nya sebelum paginasi
  http.get(
    route('/databases/:database_id/executions'),
    secured((ctx) => {
      const connection = authorizedConnection(ctx)
      requireWorkflow(connection)
      const flag = ctx.url.searchParams.get('hide_rollback')
      if (flag !== null && flag !== 'true' && flag !== 'false') throw badRequest()
      const executions = visibleExecutions([...databaseExecutions(connection.id)].reverse(), flag === 'true')
      return ok(pageOf(executions.map(toExecutionSummary), readPaging(ctx.url, ['hide_rollback'])))
    }, { query: [...pagingKeys, 'hide_rollback'] }),
  ),

  http.get(
    route('/databases/:database_id/position'),
    secured((ctx) => {
      const connection = authorizedConnection(ctx)
      requireWorkflow(connection)
      return ok(computePosition(connection.id))
    }),
  ),

  http.get(
    route('/databases/:database_id/executions/:execution_id'),
    secured((ctx) => {
      const connection = authorizedConnection(ctx)
      requireWorkflow(connection)
      return ok(toExecution(findExecution(ctx, connection)))
    }),
  ),

  http.get(
    route('/databases/:database_id/executions/:execution_id/statements'),
    secured((ctx) => {
      const connection = authorizedConnection(ctx)
      requireWorkflow(connection)
      return ok(pageOf(findExecution(ctx, connection).statements, readPaging(ctx.url)))
    }, { query: pagingKeys }),
  ),
]
