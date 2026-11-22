import type { Schemas } from '../api/types.ts'
import { sqlProblem } from './sql.ts'
import type { MockTable } from './state.ts'

type Change = Schemas['SchemaChange']

const columnSql = (column: Schemas['Column']) => `${column.name} ${column.data_type}${column.nullable ? '' : ' NOT NULL'}${column.default ? ` DEFAULT ${column.default}` : ''}`

// Hanya tabel biasa yang dibandingkan dan setiap perubahan membawa SQL kandidat yang tidak dijalankan otomatis
export function diffSchemas(current: MockTable[], desired: MockTable[], currentSchema: string, desiredSchema: string): Pick<Schemas['SchemaDiff'], 'changes' | 'warnings' | 'generated_up_sql' | 'validation' | 'destructive_count'> {
  const changes: Change[] = []
  const statements: string[] = []
  const tablesOf = (tables: MockTable[], schema: string) => tables.filter((table) => table.schema === schema && table.kind === 'table')
  const before = tablesOf(current, currentSchema)
  const after = tablesOf(desired, desiredSchema)
  for (const table of after) {
    const existing = before.find((candidate) => candidate.name === table.name)
    const qualified = `${currentSchema}.${table.name}`
    if (!existing) {
      const definition = `CREATE TABLE ${qualified} (${table.columns.map(columnSql).join(', ')})`
      changes.push({ action: 'add', object: 'table', name: table.name, before: '', after: definition, destructive: false })
      statements.push(`${definition};`)
      continue
    }
    for (const column of table.columns) {
      const old = existing.columns.find((candidate) => candidate.name === column.name)
      const name = `${table.name}.${column.name}`
      if (!old) {
        changes.push({ action: 'add', object: 'column', name, before: '', after: columnSql(column), destructive: false })
        statements.push(`ALTER TABLE ${qualified} ADD COLUMN ${columnSql(column)};`)
      } else if (old.data_type !== column.data_type) {
        changes.push({ action: 'alter', object: 'column', name, before: old.data_type, after: column.data_type, destructive: false })
        statements.push(`ALTER TABLE ${qualified} ALTER COLUMN ${column.name} TYPE ${column.data_type};`)
      } else if (old.nullable !== column.nullable) {
        changes.push({ action: 'alter', object: 'column', name, before: old.nullable ? 'NULL' : 'NOT NULL', after: column.nullable ? 'NULL' : 'NOT NULL', destructive: false })
        statements.push(`ALTER TABLE ${qualified} ALTER COLUMN ${column.name} ${column.nullable ? 'DROP' : 'SET'} NOT NULL;`)
      }
    }
    for (const old of existing.columns.filter((column) => !table.columns.some((candidate) => candidate.name === column.name))) {
      changes.push({ action: 'drop', object: 'column', name: `${table.name}.${old.name}`, before: columnSql(old), after: '', destructive: true })
      statements.push(`ALTER TABLE ${qualified} DROP COLUMN ${old.name};`)
    }
  }
  for (const table of before.filter((candidate) => !after.some((other) => other.name === candidate.name))) {
    changes.push({ action: 'drop', object: 'table', name: table.name, before: `CREATE TABLE ${currentSchema}.${table.name} (${table.columns.map(columnSql).join(', ')})`, after: '', destructive: true })
    statements.push(`DROP TABLE ${currentSchema}.${table.name};`)
  }
  const destructive = changes.filter((change) => change.destructive)
  const sql = statements.join('\n')
  return {
    changes,
    warnings: destructive.map((change) => `Perubahan destruktif pada ${change.object} ${change.name} akan menghapus data`),
    generated_up_sql: sql,
    validation: sql === '' ? 'Tidak ada perbedaan' : (sqlProblem(sql, 'normal') ?? 'SQL hasil diff lolos validasi'),
    destructive_count: destructive.length,
  }
}
