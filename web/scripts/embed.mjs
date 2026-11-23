import { cpSync, existsSync, mkdirSync, readdirSync, rmSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

const source = fileURLToPath(new URL('../dist', import.meta.url))
const target = fileURLToPath(new URL('../../src/internal/spa/dist', import.meta.url))

if (!existsSync(join(source, 'index.html'))) {
  console.error('web/dist belum berisi hasil build, jalankan npm run build lebih dulu')
  process.exit(1)
}

// Isi lama dibuang kecuali .gitkeep supaya file hasil build sebelumnya tidak tertinggal
mkdirSync(target, { recursive: true })
for (const entry of readdirSync(target)) {
  if (entry !== '.gitkeep') rmSync(join(target, entry), { recursive: true, force: true })
}
cpSync(source, target, { recursive: true })
console.log(`hasil build disalin ke ${target}`)
