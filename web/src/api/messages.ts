import { isApiError } from './errors.ts'

const defaults: Record<string, string> = {
  network: 'Tidak dapat terhubung ke server. Periksa koneksi lalu coba lagi.',
  unauthenticated: 'Sesi Anda sudah berakhir. Silakan masuk kembali.',
  forbidden: 'Anda tidak memiliki izin untuk menjalankan aksi ini.',
  csrf_failed: 'Sesi keamanan kedaluwarsa. Muat ulang halaman lalu coba lagi.',
  not_found: 'Data tidak ditemukan.',
  conflict: 'Data sudah diubah oleh pengguna lain. Muat ulang untuk melihat versi terbaru.',
  validation_failed: 'Periksa kembali isian yang ditandai.',
  internal: 'Terjadi kesalahan pada server. Coba lagi beberapa saat lagi.',
}

// Pesan dari server dipakai hanya untuk konflik dan validasi; kode lain memakai pesan generik
// supaya detail internal tidak tampil ke pengguna.
export function errorMessage(err: unknown): string {
  if (!isApiError(err)) return defaults.internal
  if (err.code === 'rate_limited') {
    return err.retryAfter ? `Terlalu banyak percobaan. Coba lagi dalam ${err.retryAfter} detik.` : 'Terlalu banyak percobaan. Tunggu sebentar lalu coba lagi.'
  }
  if ((err.code === 'conflict' || err.code === 'validation_failed') && err.message) return err.message
  return defaults[err.code] ?? defaults.internal
}

export function fieldError(err: unknown, field: string): string | undefined {
  return isApiError(err) ? err.fields[field] : undefined
}
