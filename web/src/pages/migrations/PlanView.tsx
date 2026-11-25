import type { Schemas } from '../../api/types.ts'
import { formatMetric } from '../../lib/format.ts'
import { flattenPlan } from '../../lib/plan.ts'

const dash = (value: string | undefined) => value || '-'

export function PlanView({ plan }: { plan: Schemas['Plan'] }) {
  const nodes = flattenPlan(plan.plan)
  return (
    <div className="mt-3">
      <dl className="facts mb-3">
        <dt>Statement</dt>
        <dd className="mono">
          {plan.statement} ({plan.direction}){plan.analyze ? ' dengan ANALYZE' : ''}
        </dd>
        <dt>Revision</dt>
        <dd className="mono">{plan.revision}</dd>
        <dt>Planning time (ms)</dt>
        <dd className="mono">{formatMetric(plan.planning_ms)}</dd>
        <dt>Execution time (ms)</dt>
        <dd className="mono">{formatMetric(plan.execution_ms)}</dd>
        <dt>Checksum definisi</dt>
        <dd className="mono small">{plan.checksum}</dd>
      </dl>
      <p className="text-muted small">Cost adalah unit estimasi planner, bukan milidetik. Actual rows dan actual time per node adalah rata-rata per loop, lihat loops untuk total pekerjaan. Tanda - berarti metrik tidak tersedia.</p>
      <div className="table-responsive" tabIndex={0} role="region" aria-label="Node plan">
        <table className="table table-sm table-hover table-plan" aria-label={`Node plan statement ${plan.statement}`}>
          <thead>
            <tr>
              <th scope="col">Node</th>
              <th scope="col">Relation</th>
              <th scope="col">Index</th>
              <th scope="col">Join / strategy</th>
              <th scope="col" className="text-end">
                Startup cost
              </th>
              <th scope="col" className="text-end">
                Total cost
              </th>
              <th scope="col" className="text-end">
                Estimated rows
              </th>
              <th scope="col" className="text-end">
                Actual total (ms)
              </th>
              <th scope="col" className="text-end">
                Actual rows
              </th>
              <th scope="col" className="text-end">
                Loops
              </th>
            </tr>
          </thead>
          <tbody>
            {nodes.map(({ node, depth }, index) => (
              <tr key={index}>
                <td className="mono">
                  <span style={{ paddingLeft: `${depth * 1.25}rem` }}>
                    {depth > 0 && <span aria-hidden="true">↳ </span>}
                    {node.node_type}
                  </span>
                </td>
                <td className="mono">{dash(node.relation)}</td>
                <td className="mono">{dash(node.index)}</td>
                <td>{dash([node.join, node.strategy].filter(Boolean).join(' / '))}</td>
                <td className="mono text-end">{formatMetric(node.startup_cost)}</td>
                <td className="mono text-end">{formatMetric(node.total_cost)}</td>
                <td className="mono text-end">{formatMetric(node.rows)}</td>
                <td className="mono text-end">{formatMetric(node.actual_total_ms)}</td>
                <td className="mono text-end">{formatMetric(node.actual_rows)}</td>
                <td className="mono text-end">{formatMetric(node.actual_loops)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <details className="mt-3">
        <summary className="small">JSON plan terstruktur</summary>
        <pre className="code-block mono mt-2">{JSON.stringify(plan.plan, null, 2)}</pre>
      </details>
    </div>
  )
}
