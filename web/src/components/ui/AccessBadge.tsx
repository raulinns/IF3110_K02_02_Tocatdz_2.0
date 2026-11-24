import type { Schemas } from '../../api/types.ts'
import { accessLabel } from '../../lib/labels.ts'

export function AccessBadge({ access }: { access: Schemas['Access'] }) {
  return <span className={`badge access-${access}`}>{accessLabel(access)}</span>
}
