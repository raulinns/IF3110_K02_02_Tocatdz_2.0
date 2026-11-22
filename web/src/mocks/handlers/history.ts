import { http } from 'msw/http'
import { authorizedConnection, requireWorkflow } from '../access.ts'
import { badRequest, notFound, ok, pageOf, pagingKeys, readPaging, route, secured, type Context } from '../http.ts'
import { toExecution, toExecutionSummary } from '../models.ts'
import { computePosition, databaseExecutions, visibleExecutions } from '../position.ts'
import { getState, type MockConnection, type MockExecution } from '../state.ts'

function findExecution({ params }: Context, connection: MockConnection): MockExecution {
  const execution = getState().executions.find((candidate) => candidate.id === params.execution_id && candidate.database_id === connection.id)
  if (!execution) throw notFound()
  return execution
}

export const historyHandlers = [
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
