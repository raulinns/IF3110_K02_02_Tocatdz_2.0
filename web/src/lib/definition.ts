import type { Definition } from '../api/migrations.ts'
import type { Schemas } from '../api/types.ts'

export interface BatchForm {
  schema: string
  table: string
  keyColumn: string
  batchSize: string
  scopeWhere: string
}

// Isi form disimpan sebagai teks mentah supaya ketikan pengguna tidak diubah sebelum dikirim
export interface DefinitionForm {
  title: string
  upSql: string
  downSql: string
  irreversible: boolean
  reason: string
  mode: Schemas['ExecutionMode']
  batch: BatchForm
}

// Pesan per field dengan kunci yang sama seperti path field pada error validasi API
export type Problems = Record<string, string[]>

const identifierPattern = /^[A-Za-z_][A-Za-z0-9_$]*$/
const maxTitleLength = 200
const maxSqlBytes = 524288
const byteLength = (text: string) => new TextEncoder().encode(text).length
const length = (text: string) => Array.from(text).length

export const emptyBatch: BatchForm = { schema: 'public', table: '', keyColumn: 'id', batchSize: '1000', scopeWhere: '' }

export const emptyForm = (): DefinitionForm => ({ title: '', upSql: '', downSql: '', irreversible: false, reason: '', mode: 'normal', batch: { ...emptyBatch } })

export function formFromMigration(migration: Definition): DefinitionForm {
  const batch = migration.batch
  return {
    title: migration.title,
    upSql: migration.up_sql,
    downSql: migration.down_sql ?? '',
    irreversible: migration.irreversible,
    reason: migration.irreversible_reason,
    mode: migration.mode,
    batch: batch ? { schema: batch.schema, table: batch.table, keyColumn: batch.key_column, batchSize: String(batch.batch_size), scopeWhere: batch.scope_where ?? '' } : { ...emptyBatch },
  }
}

// Judul dan SQL dikirim apa adanya karena byte yang sudah ditinjau tidak boleh berubah, dan predikat kosong menjadi null
export function toDefinition(form: DefinitionForm): Definition {
  const batch = form.batch
  return {
    title: form.title,
    up_sql: form.upSql,
    down_sql: form.irreversible ? null : form.downSql,
    irreversible: form.irreversible,
    irreversible_reason: form.irreversible ? form.reason : '',
    mode: form.mode,
    batch:
      form.mode === 'batch'
        ? { schema: batch.schema, table: batch.table, key_column: batch.keyColumn, batch_size: Number(batch.batchSize), scope_where: batch.scopeWhere.trim() === '' ? null : batch.scopeWhere }
        : null,
  }
}

export const sameForm = (a: DefinitionForm, b: DefinitionForm) => JSON.stringify(a) === JSON.stringify(b)

function sqlProblem(sql: string, label: string): string | undefined {
  if (sql.trim() === '') return `${label} wajib diisi`
  return byteLength(sql) > maxSqlBytes ? `${label} maksimal ${maxSqlBytes} byte` : undefined
}

function identifierProblem(value: string, label: string): string | undefined {
  if (value.trim() === '') return `${label} wajib diisi`
  return byteLength(value) > 63 || !identifierPattern.test(value) ? `${label} harus satu identifier valid, maksimal 63 byte` : undefined
}

// Pemeriksaan cepat untuk isian yang jelas salah, sedangkan validasi SQL dan batch yang sesungguhnya tetap dilakukan server
export function definitionProblems(form: DefinitionForm): Problems {
  const found: Record<string, string | undefined> = {}
  const title = form.title.trim()
  found.title = title === '' ? 'Judul wajib diisi' : length(title) > maxTitleLength ? `Judul maksimal ${maxTitleLength} karakter` : undefined
  found.up_sql = sqlProblem(form.upSql, form.mode === 'batch' ? 'Bagian SET' : 'SQL up')
  if (form.irreversible) {
    found.irreversible_reason = form.reason.trim() === '' ? 'Alasan irreversible wajib diisi' : undefined
  } else {
    found.down_sql = form.downSql.trim() === '' ? 'SQL down wajib diisi atau tandai migration irreversible' : sqlProblem(form.downSql, form.mode === 'batch' ? 'Bagian SET down' : 'SQL down')
  }
  if (form.mode === 'batch') {
    const batch = form.batch
    found['batch.schema'] = identifierProblem(batch.schema, 'Schema')
    found['batch.table'] = identifierProblem(batch.table, 'Tabel')
    found['batch.key_column'] = identifierProblem(batch.keyColumn, 'Kolom kunci')
    const size = Number(batch.batchSize)
    found['batch.batch_size'] = batch.batchSize.trim() !== '' && Number.isInteger(size) && size >= 100 && size <= 100000 ? undefined : 'Ukuran batch harus bilangan bulat 100 sampai 100000'
  }
  const problems: Problems = {}
  for (const [field, message] of Object.entries(found)) if (message) problems[field] = [message]
  return problems
}

// Urutan field di form dipakai untuk memindahkan fokus ke isian pertama yang bermasalah
export const fieldOrder = ['title', 'mode', 'up_sql', 'down_sql', 'irreversible_reason', 'batch', 'batch.schema', 'batch.table', 'batch.key_column', 'batch.batch_size', 'batch.scope_where']

export const firstProblem = (problems: Problems) => fieldOrder.find((field) => problems[field]?.length)
