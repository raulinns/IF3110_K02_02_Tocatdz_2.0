import { scanSql } from '../lib/sql.ts'

export { splitStatements } from '../lib/sql.ts'

export type SqlMode = 'normal' | 'batch'

const messages = {
  syntax: 'SQL tidak valid. Periksa sintaks DBMS target.',
  transaction: 'Perintah transaksi diatur oleh aplikasi.',
  concurrent: 'Perintah ini tidak dapat dijalankan dalam transaksi migration.',
  protected: 'SQL mengakses objek internal atau schema sistem.',
  unsupported: 'Jenis perintah SQL ini belum diizinkan untuk migration.',
  batchSingle: 'Mode batch hanya menerima satu bagian SET.',
  batchKeyword: 'Isi hanya bagian SET tanpa kata UPDATE, SET, INSERT, atau DELETE.',
  batchShape: 'Gunakan bentuk kolom = ekspresi, misalnya status = \'arsip\'.',
}

const transactionKeywords = new Set(['begin', 'commit', 'rollback', 'start', 'end', 'savepoint', 'release', 'abort'])
const unsupportedKeywords = new Set(['copy', 'grant', 'revoke', 'do', 'call', 'listen', 'notify', 'lock', 'load', 'set', 'reset', 'discard'])
const unsupportedObjects = /^(create|alter|drop)\s+(role|user|database|extension|tablespace|system)\b/i
const protectedObjects = /\b(information_schema|pg_toast)\b|\bpg_catalog\s*\.\s*pg_|_tocatdz_/i

function unbalanced(text: string): boolean {
  let depth = 0
  for (const ch of text) {
    if (ch === '(') depth += 1
    if (ch === ')') depth -= 1
    if (depth < 0) return true
  }
  return depth !== 0
}

// Pemeriksaan heuristik yang meniru kategori penolakan validator M1 dan mengembalikan pesan, atau null jika lolos
export function sqlProblem(sql: string, mode: SqlMode): string | null {
  const { stripped, unterminated } = scanSql(sql)
  if (unterminated || stripped.some(unbalanced)) return messages.syntax
  if (mode === 'batch') return batchProblem(stripped)
  for (const statement of stripped) {
    const keyword = statement.split(/\s+/)[0].toLowerCase()
    if (transactionKeywords.has(keyword)) return messages.transaction
    if (keyword === 'vacuum' || /\bconcurrently\b/i.test(statement)) return messages.concurrent
    if (protectedObjects.test(statement)) return messages.protected
    if (unsupportedKeywords.has(keyword) || unsupportedObjects.test(statement)) return messages.unsupported
  }
  return null
}

function batchProblem(stripped: string[]): string | null {
  if (stripped.length !== 1) return messages.batchSingle
  const [assignment] = stripped
  if (/^(update|set|insert|delete)\b/i.test(assignment)) return messages.batchKeyword
  if (!/^[a-z_][a-z0-9_]*\s*=/i.test(assignment)) return messages.batchShape
  return protectedObjects.test(assignment) ? messages.protected : null
}

// Literal string diganti penanda seperti pada SQL yang tercatat di riwayat eksekusi
export function redactLiterals(sql: string): string {
  return sql.replace(/'(?:[^']|'')*'/g, "'[REDACTED]'")
}

export function firstKeyword(statement: string): string {
  return statement.trim().split(/\s+/)[0]?.toLowerCase() ?? ''
}
