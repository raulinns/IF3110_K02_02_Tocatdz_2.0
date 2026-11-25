import { useEffect, useState } from 'react'

// Waktu sekarang yang diperbarui tiap detik selama aktif, dipakai untuk durasi job yang masih berjalan
export function useNow(active: boolean): number {
  const [now, setNow] = useState(() => Date.now())
  useEffect(() => {
    if (!active) return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [active])
  return now
}
