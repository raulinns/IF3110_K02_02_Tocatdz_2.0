// Muat aplikasi satu kali sebelum test dimulai karena pemuatan pertama server dev bisa memakan puluhan detik dan memperlambat test yang kebetulan berjalan lebih dulu
export default async function warmup() {
  const base = 'http://localhost:5200'
  for (const path of ['/login', '/src/main.tsx']) {
    await fetch(`${base}${path}`).catch(() => undefined)
  }
}
