import { http } from 'msw/http'
import type { Schemas } from '../../api/types.ts'
import { authorizedConnection } from '../access.ts'
import { badRequest, notFound, ok, pageOf, pagingKeys, readPaging, route, secured, sortBy, utf8Length, type Context } from '../http.ts'
import { capabilities } from '../models.ts'
import { accessFor, getState, type MockConnection, type MockTable } from '../state.ts'

const maxCellLength = 4096
const maxIdentifierBytes = 63

function findTable({ params }: Context, connection: MockConnection): MockTable {
  if (utf8Length(params.schema) > maxIdentifierBytes || utf8Length(params.table) > maxIdentifierBytes) throw badRequest()
  const table = getState().tables.find((candidate) => candidate.database_id === connection.id && candidate.schema === params.schema && candidate.name === params.table)
  if (!table) throw notFound()
  return table
}

// Nilai NULL ditandai terpisah, bytea tampil sebagai hex, dan teks panjang dipotong dengan penanda
function toCell(value: string | null, dataType: string): Schemas['Cell'] {
  if (value === null) return { text: '', null: true, truncated: false, encoding: 'text' }
  const truncated = value.length > maxCellLength
  return { text: truncated ? value.slice(0, maxCellLength) : value, null: false, truncated, encoding: dataType === 'bytea' ? 'hex' : 'text' }
}

export const explorerHandlers = [
  http.get(
    route('/databases'),
    secured(({ url, user }) => {
      const items = getState()
        .connections.flatMap((connection): Schemas['Database'][] => {
          const access = accessFor(user.id, connection.id)
          return access ? [{ id: connection.id, name: connection.name, engine: connection.engine, access, capabilities: capabilities(connection.engine) }] : []
        })
        .sort(sortBy<Schemas['Database']>((database) => database.name, (database) => database.id))
      return ok(pageOf(items, readPaging(url)))
    }, { query: pagingKeys }),
  ),

  http.get(
    route('/databases/:database_id/schema'),
    secured((ctx) => {
      const connection = authorizedConnection(ctx)
      const tables = getState().tables.filter((table) => table.database_id === connection.id)
      const schemas = [...new Set(tables.map((table) => table.schema))].sort().map((name) => ({
        name,
        tables: tables
          .filter((table) => table.schema === name)
          .map((table) => ({ name: table.name, kind: table.kind }))
          .sort(sortBy<Schemas['TableSummary']>((table) => table.name)),
      }))
      return ok({ schemas })
    }),
  ),

  http.get(
    route('/databases/:database_id/tables/:schema/:table'),
    secured((ctx) => {
      const table = findTable(ctx, authorizedConnection(ctx))
      return ok({ schema: table.schema, name: table.name, kind: table.kind, columns: table.columns, constraints: table.constraints, indexes: table.indexes })
    }),
  ),

  http.get(
    route('/databases/:database_id/tables/:schema/:table/rows'),
    secured((ctx) => {
      const table = findTable(ctx, authorizedConnection(ctx))
      const window = pageOf(table.rows, readPaging(ctx.url))
      return ok({
        columns: table.columns.map((column) => column.name),
        items: window.items.map((row) => row.map((value, index) => toCell(value, table.columns[index].data_type))),
        page: window.page,
        limit: window.limit,
        has_next: window.has_next,
        order: table.order,
      })
    }, { query: pagingKeys }),
  ),
]
