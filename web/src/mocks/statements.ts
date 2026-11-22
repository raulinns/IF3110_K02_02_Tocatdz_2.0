import type { Schemas } from '../api/types.ts'
import { redactLiterals, splitStatements } from './sql.ts'

// Mode batch membentuk satu UPDATE per batch sesuai format kontrak, sedangkan mode normal mencatat tiap statement apa adanya
export function buildStatements(mode: Schemas['ExecutionMode'], batch: Schemas['BatchConfig'] | null, sql: string, batchRows: number[]): Schemas['ExecutedStatement'][] {
  if (mode === 'batch' && batch) {
    const { schema, table, key_column: key, batch_size: size, scope_where: scope } = batch
    const filter = scope ? ` AND (${redactLiterals(scope)})` : ''
    const query = `UPDATE ${schema}.${table} SET ${redactLiterals(sql)} WHERE ${key} IN (SELECT ${key} FROM ${schema}.${table} WHERE ${key} > $1 AND ${key} <= $2${filter} ORDER BY ${key} LIMIT ${size}) RETURNING ${key}`
    return batchRows.map((_, index) => ({ query, params: ['[REDACTED]', '[REDACTED]'], batch: index + 1 }))
  }
  return splitStatements(sql).map((statement) => ({ query: redactLiterals(statement), params: [], batch: null }))
}
