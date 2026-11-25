export interface Scan {
  statements: string[]
  stripped: string[]
  unterminated: boolean
}

// Komentar dibuang dan isi string diganti kosong supaya pemeriksaan tidak salah mengenali kata di dalam literal
export function scanSql(sql: string): Scan {
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

// Memecah SQL menjadi statement tanpa komentar, dipakai untuk pemilihan statement EXPLAIN dan daftar statement
export function splitStatements(sql: string): string[] {
  return scanSql(sql).statements
}
