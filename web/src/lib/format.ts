const formatter = new Intl.DateTimeFormat('id-ID', { dateStyle: 'medium', timeStyle: 'short' })

// Waktu dari server berformat RFC 3339 UTC dan ditampilkan dalam zona waktu peramban
export function formatDateTime(iso: string): string {
  const date = new Date(iso)
  return Number.isNaN(date.getTime()) ? iso : formatter.format(date)
}

// Angka metrik plan dibulatkan supaya tabel tetap ringkas, dan nilai yang tidak tersedia ditandai garis
export function formatMetric(value: number | null | undefined): string {
  return value === null || value === undefined ? '-' : String(Number(value.toFixed(3)))
}
