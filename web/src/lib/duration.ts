// Durasi singkat dalam bahasa Indonesia untuk waktu tunggu antrean dan lama eksekusi
export function formatDuration(ms: number): string {
  if (!Number.isFinite(ms) || ms < 0) return '-'
  if (ms < 1000) return `${Math.round(ms)} ms`
  const seconds = ms / 1000
  if (seconds < 60) return `${Number(seconds.toFixed(1))} dtk`
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes} mnt ${Math.floor(seconds % 60)} dtk`
  return `${Math.floor(minutes / 60)} jam ${minutes % 60} mnt`
}

// Selisih dua waktu RFC 3339, dan waktu akhir yang kosong berarti masih berjalan sampai sekarang
export function elapsed(from: string | null, to: string | null, now: number): number | null {
  if (!from) return null
  const start = Date.parse(from)
  const end = to ? Date.parse(to) : now
  return Number.isNaN(start) || Number.isNaN(end) ? null : Math.max(0, end - start)
}
