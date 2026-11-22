import { isApiError, type ApiErrorCode } from './errors.ts'

const defaults: Record<ApiErrorCode, string> = {
  network: 'Tidak dapat terhubung ke server. Periksa koneksi lalu coba lagi.',
  bad_request: 'Permintaan tidak valid. Muat ulang halaman lalu coba lagi.',
  unauthenticated: 'Sesi Anda sudah berakhir. Silakan masuk kembali.',
  forbidden: 'Anda tidak memiliki izin untuk menjalankan aksi ini.',
  csrf_failed: 'Sesi keamanan kedaluwarsa. Muat ulang halaman lalu coba lagi.',
  not_found: 'Data tidak ditemukan.',
  method_not_allowed: 'Aksi ini tidak tersedia.',
  conflict: 'Data sudah diubah oleh pengguna lain. Muat ulang untuk melihat versi terbaru.',
  validation_failed: 'Periksa kembali isian yang ditandai.',
  payload_too_large: 'Data yang dikirim terlalu besar.',
  unsupported_media_type: 'Format data yang dikirim tidak didukung.',
  result_too_large: 'Hasil terlalu besar. Kurangi jumlah data yang diminta.',
  rate_limited: 'Terlalu banyak percobaan. Tunggu sebentar lalu coba lagi.',
  unavailable: 'Layanan sementara tidak tersedia. Coba lagi beberapa saat lagi.',
  internal: 'Terjadi kesalahan pada server. Coba lagi beberapa saat lagi.',
}

// Alasan domain dari server, dipakai sebagai pengganti pesan generik
const reasons = new Map([
  ['revision_mismatch', 'Migration sudah diubah. Muat ulang untuk melihat revisi terbaru.'],
  ['self_approval', 'Pengaju tidak dapat menyetujui permintaannya sendiri.'],
  ['missing_leader', 'Ada group terkait yang belum memiliki leader, sehingga permintaan belum bisa diproses.'],
  ['stale_approval', 'Daftar approver berubah sejak permintaan diajukan. Ajukan ulang permintaan ini.'],
  ['position_changed', 'Posisi migration sudah berubah. Muat ulang lalu ajukan ulang.'],
  ['irreversible', 'Migration ini ditandai tidak dapat dibatalkan.'],
  ['database_blocked', 'Database sedang ditahan oleh migration lain yang belum selesai.'],
  ['unsafe_revert', 'Pembatalan batch ini tidak aman untuk dijalankan.'],
])

// Pesan server dipakai hanya untuk konflik dan validasi supaya detail internal tidak tampil
export function errorMessage(err: unknown): string {
  if (!isApiError(err)) return defaults.internal
  if (err.code === 'rate_limited') {
    return err.retryAfter ? `Terlalu banyak percobaan. Coba lagi dalam ${err.retryAfter} detik.` : defaults.rate_limited
  }
  const byReason = err.reason ? reasons.get(err.reason) : undefined
  if (byReason) return byReason
  if ((err.code === 'conflict' || err.code === 'validation_failed') && err.message) return err.message
  return defaults[err.code]
}

// Satu field bisa membawa beberapa pesan, misalnya beberapa temuan validator pada satu editor SQL
export function fieldErrors(err: unknown, field: string): string[] {
  return isApiError(err) && Object.hasOwn(err.fields, field) ? err.fields[field] : []
}

export function fieldError(err: unknown, field: string): string | undefined {
  return fieldErrors(err, field)[0]
}
