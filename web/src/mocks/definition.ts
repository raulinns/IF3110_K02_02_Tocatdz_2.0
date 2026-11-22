import type { Schemas } from '../api/types.ts'
import type { Definition } from './checksum.ts'
import { badRequest, stringField, utf8Length, type JsonObject } from './http.ts'
import { splitStatements, sqlProblem, type SqlMode } from './sql.ts'
import { Fields } from './validation.ts'

const maxSqlBytes = 524288
const maxTitleRunes = 200
const maxScopeLength = 4096
const identifierPattern = /^[A-Za-z_][A-Za-z0-9_$]*$/
const batchKeys = ['schema', 'table', 'key_column', 'batch_size', 'scope_where']

function readBatch(raw: unknown): Schemas['BatchConfig'] | null {
  if (raw === undefined || raw === null) return null
  if (typeof raw !== 'object' || Array.isArray(raw)) throw badRequest()
  const body = raw as JsonObject
  if (Object.keys(body).some((key) => !batchKeys.includes(key))) throw badRequest()
  const size = body.batch_size
  const scope = body.scope_where
  if (size !== undefined && typeof size !== 'number') throw badRequest()
  if (scope !== undefined && scope !== null && typeof scope !== 'string') throw badRequest()
  return {
    schema: stringField(body, 'schema') ?? '',
    table: stringField(body, 'table') ?? '',
    key_column: stringField(body, 'key_column') ?? '',
    batch_size: size ?? 0,
    scope_where: scope ?? null,
  }
}

// Tipe yang salah adalah bad_request, sedangkan field yang kosong atau tidak masuk akal ditandai oleh validateDefinition
export function readDefinition(body: JsonObject): Definition {
  const irreversible = body.irreversible
  const down = body.down_sql
  if (irreversible !== undefined && typeof irreversible !== 'boolean') throw badRequest()
  if (down !== undefined && down !== null && typeof down !== 'string') throw badRequest()
  return {
    title: stringField(body, 'title') ?? '',
    up_sql: stringField(body, 'up_sql') ?? '',
    down_sql: down ?? null,
    irreversible: irreversible ?? false,
    irreversible_reason: stringField(body, 'irreversible_reason') ?? '',
    mode: (stringField(body, 'mode') ?? '') as Definition['mode'],
    batch: readBatch(body.batch),
  }
}

function sqlFieldError(sql: string, label: string, mode: SqlMode): string | null {
  if (sql.trim() === '' || splitStatements(sql).length === 0) return `${label} wajib diisi`
  if (utf8Length(sql) > maxSqlBytes) return `${label} maksimal ${maxSqlBytes} byte`
  return sqlProblem(sql, mode)
}

function identifierError(value: string, label: string): string | null {
  if (value === '') return `${label} wajib diisi`
  if (utf8Length(value) > 63 || !identifierPattern.test(value)) return `${label} harus satu identifier valid, maksimal 63 byte`
  return null
}

function scopeError(scope: string | null): string | null {
  if (scope === null || scope === '') return null
  if (scope.length > maxScopeLength) return 'Predikat terlalu panjang'
  if (/;|--|\/\*|\b(select|insert|update|delete|drop|truncate)\b/i.test(scope)) return 'Predikat harus berupa satu ekspresi tanpa subquery atau perintah lain'
  return null
}

// Aturan definisi migration: SQL up dan down, irreversible dengan alasan, serta konfigurasi batch
export function validateDefinition(definition: Definition, fields: Fields) {
  const title = definition.title.trim()
  fields.add('title', title === '' ? 'Judul wajib diisi' : Array.from(title).length > maxTitleRunes ? `Judul maksimal ${maxTitleRunes} karakter` : null)
  fields.add('mode', definition.mode === 'normal' || definition.mode === 'batch' ? null : 'Mode harus normal atau batch')
  const sqlMode: SqlMode = definition.mode === 'batch' ? 'batch' : 'normal'
  fields.add('up_sql', sqlFieldError(definition.up_sql, 'SQL up', sqlMode))
  if (definition.irreversible) {
    fields.add('down_sql', definition.down_sql === null ? null : 'Migration irreversible tidak memiliki SQL down')
    fields.add('irreversible_reason', definition.irreversible_reason.trim() === '' ? 'Alasan irreversible wajib diisi' : null)
  } else {
    fields.add('down_sql', definition.down_sql === null ? 'SQL down wajib diisi atau tandai migration irreversible' : sqlFieldError(definition.down_sql, 'SQL down', sqlMode))
    fields.add('irreversible_reason', definition.irreversible_reason === '' ? null : 'Alasan hanya dipakai untuk migration irreversible')
  }
  const batch = definition.batch
  if (definition.mode === 'batch') {
    if (!batch) {
      fields.add('batch', 'Konfigurasi batch wajib diisi')
      return
    }
    fields.add('batch.schema', identifierError(batch.schema, 'Schema'))
    fields.add('batch.table', identifierError(batch.table, 'Tabel'))
    fields.add('batch.key_column', identifierError(batch.key_column, 'Kolom kunci'))
    fields.add('batch.batch_size', Number.isInteger(batch.batch_size) && batch.batch_size >= 100 && batch.batch_size <= 100000 ? null : 'Ukuran batch harus bilangan bulat 100 sampai 100000')
    fields.add('batch.scope_where', scopeError(batch.scope_where))
  } else if (definition.mode === 'normal' && batch !== null) {
    fields.add('batch', 'Mode normal tidak memakai konfigurasi batch')
  }
}

// Hanya judul yang dirapikan karena byte SQL yang sudah ditinjau tidak boleh berubah
export function normalizeDefinition(definition: Definition): Definition {
  return { ...definition, title: definition.title.trim() }
}
