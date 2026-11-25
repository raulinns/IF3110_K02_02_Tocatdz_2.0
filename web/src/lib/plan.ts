import type { Schemas } from '../api/types.ts'

type PlanNode = Schemas['PlanNode']

export interface FlatNode {
  node: PlanNode
  depth: number
}

// Pohon plan diratakan dengan kedalamannya supaya tampil sebagai satu tabel yang bisa digulir
export function flattenPlan(root: PlanNode): FlatNode[] {
  const result: FlatNode[] = []
  const visit = (node: PlanNode, depth: number) => {
    result.push({ node, depth })
    node.plans?.forEach((child) => visit(child, depth + 1))
  }
  visit(root, 0)
  return result
}
