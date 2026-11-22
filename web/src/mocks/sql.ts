export type SqlMode = 'normal' | 'batch'

interface Scan {
  statements: string[]
  stripped: string[]
  unterminated: boolean
}

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

// Komentar dibuang dan isi string diganti kosong supaya pemeriksaan tidak salah mengenali kata di dalam literal
function scan(sql: string): Scan {
  const statements: string[] = []
  const stripped: string[] = []
  let raw = ''
  let clean = ''
  let unterminated = false
  const flush = () => {
    if (raw.trim() !== '') {
      statements.push(raw.trim())
      stripped.push(clean.trim())
    }
    raw = ''
    clean = ''
  }
  let i = 0
  while (i < sql.length) {
    const ch = sql[i]
    const next = sql[i + 1]
    if (ch === '-' && next === '-') {
      const end = sql.indexOf('\n', i)
      i = end < 0 ? sql.length : end
    } else if (ch === '/' && next === '*') {
      const end = sql.indexOf('*/', i + 2)
      if (end < 0) unterminated = true
      i = end < 0 ? sql.length : end + 2
      raw += ' '
      clean += ' '
    } else if (ch === "'" || ch === '"') {
      let j = i + 1
      while (j < sql.length && !(sql[j] === ch && sql[j + 1] !== ch)) j += sql[j] === ch ? 2 : 1
      if (j >= sql.length) unterminated = true
      const literal = sql.slice(i, j + 1)
      raw += literal
      clean += ch === "'" ? "''" : literal
      i = j + 1
    } else if (ch === ';') {
      flush()
      i += 1
    } else {
      raw += ch
      clean += ch
      i += 1
    }
  }
  flush()
  return { statements, stripped, unterminated }
}

function unbalanced(text: string): boolean {
  let depth = 0
  for (const ch of text) {
    if (ch === '(') depth += 1
    if (ch === ')') depth -= 1
    if (depth < 0) return true
  }
  return depth !== 0
}

// Memecah SQL menjadi statement tanpa komentar, dipakai untuk pemilihan statement EXPLAIN dan daftar statement
export function splitStatements(sql: string): string[] {
  return scan(sql).statements
}

// Pemeriksaan heuristik yang meniru kategori penolakan validator M1 dan mengembalikan pesan, atau null jika lolos
export function sqlProblem(sql: string, mode: SqlMode): string | null {
  const { stripped, unterminated } = scan(sql)
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
