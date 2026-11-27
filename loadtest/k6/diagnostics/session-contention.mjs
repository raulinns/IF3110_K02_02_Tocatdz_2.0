// Mengukur apakah permintaan paralel pada satu sesi dibalas 409 oleh server, tanpa k6 dan tanpa dependensi
// Pemakaian: BASE_URL=http://localhost:8080 USERNAME=editor PASSWORD=... node loadtest/k6/diagnostics/session-contention.mjs
// Hasil yang sehat adalah semua permintaan 200 pada tiap jumlah paralel
const base = (process.env.BASE_URL ?? 'http://localhost:8080').replace(/\/+$/, '')
const api = `${base}/api/v1`
const username = process.env.USERNAME_UNDER_TEST ?? process.env.USERNAME ?? 'editor'
const password = process.env.PASSWORD ?? ''
const sizes = (process.env.PARALLEL ?? '1,5,10,20,40').split(',').map(Number)

const login = await fetch(`${api}/auth/login`, { method: 'POST', headers: { 'Content-Type': 'application/json', Origin: new URL(base).origin }, body: JSON.stringify({ username, password }) })
if (login.status !== 200) {
  console.error(`login gagal dengan status ${login.status}`)
  process.exit(1)
}
const cookie = login.headers.getSetCookie().map((value) => value.split(';')[0]).join('; ')

for (const size of sizes) {
  const started = Date.now()
  const statuses = await Promise.all(Array.from({ length: size }, () => fetch(`${api}/databases?page=1&limit=50`, { headers: { Origin: new URL(base).origin, Cookie: cookie } }).then((response) => response.status)))
  const counts = {}
  for (const status of statuses) counts[status] = (counts[status] ?? 0) + 1
  console.log(`1 sesi x ${size} paralel: ${JSON.stringify(counts)} dalam ${Date.now() - started} ms`)
}
