import { describe, expect, it } from 'vitest'
import type { Schemas } from '../api/types.ts'
import { flattenPlan } from './plan.ts'

const node = (type: string, plans?: Schemas['PlanNode'][]): Schemas['PlanNode'] => ({ node_type: type, startup_cost: 0, total_cost: 1, rows: 1, width: 4, plans })

describe('flattenPlan', () => {
  it('node tunggal berada di kedalaman nol', () => {
    expect(flattenPlan(node('Seq Scan')).map(({ node: n, depth }) => [n.node_type, depth])).toEqual([['Seq Scan', 0]])
  })

  it('anak diurutkan setelah induknya dengan kedalaman bertambah', () => {
    const tree = node('Hash Join', [node('Seq Scan'), node('Hash', [node('Index Scan')])])
    expect(flattenPlan(tree).map(({ node: n, depth }) => [n.node_type, depth])).toEqual([
      ['Hash Join', 0],
      ['Seq Scan', 1],
      ['Hash', 1],
      ['Index Scan', 2],
    ])
  })
})
