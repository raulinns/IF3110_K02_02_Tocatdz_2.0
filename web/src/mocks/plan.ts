import type { Schemas } from '../api/types.ts'
import { firstKeyword } from './sql.ts'
import { getState } from './state.ts'

type Node = Schemas['PlanNode']

const explainable = new Set(['select', 'with', 'insert', 'update', 'delete'])
const readOnlyBlockers = /\b(insert|update|delete)\b/i

export function canExplain(statement: string): boolean {
  return explainable.has(firstKeyword(statement))
}

// ANALYZE benar-benar menjalankan statement sehingga hanya SELECT yang tidak mengubah data yang diizinkan
export function isReadOnlyQuery(statement: string): boolean {
  const keyword = firstKeyword(statement)
  return keyword === 'select' || (keyword === 'with' && !readOnlyBlockers.test(statement))
}

function targetOf(statement: string): { schema: string; relation: string } {
  const match = /\b(?:from|update|into|join)\s+(?:(\w+)\.)?(\w+)/i.exec(statement)
  return { schema: match?.[1] ?? 'public', relation: match?.[2] ?? 'dual' }
}

// Biaya dan jumlah baris diturunkan dari isi tabel mock sehingga hasilnya konsisten untuk statement yang sama
function withActuals(node: Node, loops = 1): Node {
  return {
    ...node,
    actual_startup_ms: Number((node.startup_cost * 0.01 + 0.02).toFixed(3)),
    actual_total_ms: Number((node.total_cost * 0.01 + 0.05).toFixed(3)),
    actual_rows: node.rows,
    actual_loops: loops,
    plans: node.plans?.map((child) => withActuals(child, loops)),
  }
}

export function buildPlan(statement: string, databaseId: string, analyze: boolean): { plan: Node; planningMs: number | null; executionMs: number | null } {
  const keyword = firstKeyword(statement)
  const { schema, relation } = targetOf(statement)
  const table = getState().tables.find((candidate) => candidate.database_id === databaseId && candidate.name === relation)
  const rows = table ? table.rows.length : 1000
  const indexed = /\bwhere\s+(?:\w+\.)?id\s*=/i.test(statement)
  const scan: Node = indexed
    ? { node_type: 'Index Scan', relation, schema, index: `${relation}_pkey`, startup_cost: 0.15, total_cost: 8.17, rows: 1, width: 64 }
    : { node_type: 'Seq Scan', relation, schema, startup_cost: 0, total_cost: Number((rows * 1.1 + 1).toFixed(2)), rows, width: 64 }
  let root = scan
  if (keyword === 'insert') root = { node_type: 'ModifyTable', relation, schema, startup_cost: 0, total_cost: 0.01, rows: 1, width: 0, plans: [{ node_type: 'Result', startup_cost: 0, total_cost: 0.01, rows: 1, width: 32 }] }
  else if (keyword === 'update' || keyword === 'delete') root = { node_type: 'ModifyTable', relation, schema, startup_cost: scan.startup_cost, total_cost: Number((scan.total_cost + rows * 0.02).toFixed(2)), rows: scan.rows, width: 0, plans: [scan] }
  const plan = analyze ? withActuals(root) : root
  return { plan, planningMs: analyze ? 0.12 : null, executionMs: analyze ? (plan.actual_total_ms ?? 0) : null }
}
