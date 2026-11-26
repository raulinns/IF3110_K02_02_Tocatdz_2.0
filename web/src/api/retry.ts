import { isApiError } from './errors.ts'

// Hanya gangguan jaringan dan error server yang diulang sedangkan 4xx langsung ditampilkan
// Konflik pada pembacaan diulang satu kali karena pembacaan tidak bisa berselisih dengan data pengguna, dan server kadang membalasnya
// saat banyak permintaan paralel memakai satu sesi yang sedang diperbarui
export function shouldRetryQuery(count: number, err: unknown): boolean {
  if (!isApiError(err)) return false
  if (err.code === 'conflict') return count < 1
  return count < 2 && (err.code === 'network' || err.status >= 500)
}
