import type { DefinitionForm } from '../../lib/definition.ts'

// Isian awal editor dari halaman lain, dibawa lewat state router dan bukan lewat URL karena SQL bisa panjang
export interface Prefill {
  form: DefinitionForm
  source: 'diff' | 'copy'
  // Judul migration asal untuk salinan
  origin?: string
}

const isObject = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null

function isForm(value: unknown): value is DefinitionForm {
  if (!isObject(value) || !isObject(value.batch)) return false
  const strings = ['title', 'upSql', 'downSql', 'reason']
  const batch = ['schema', 'table', 'keyColumn', 'batchSize', 'scopeWhere']
  return strings.every((key) => typeof value[key] === 'string') && typeof value.irreversible === 'boolean' && (value.mode === 'normal' || value.mode === 'batch') && batch.every((key) => typeof (value.batch as Record<string, unknown>)[key] === 'string')
}

export function readPrefill(state: unknown): Prefill | null {
  if (!isObject(state) || !isObject(state.prefill)) return null
  const { form, source, origin } = state.prefill
  if (!isForm(form) || (source !== 'diff' && source !== 'copy')) return null
  return { form, source, origin: typeof origin === 'string' ? origin : undefined }
}
