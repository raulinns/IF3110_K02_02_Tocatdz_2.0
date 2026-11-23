import { useCallback, useEffect, useState } from 'react'

// Hitung mundur berbasis tenggat waktu sehingga tetap akurat walau tab sempat tidak aktif
export function useCountdown() {
  const [deadline, setDeadline] = useState<number | null>(null)
  const [now, setNow] = useState(0)

  useEffect(() => {
    if (deadline === null) return
    const timer = setInterval(() => {
      const current = Date.now()
      setNow(current)
      if (current >= deadline) setDeadline(null)
    }, 250)
    return () => clearInterval(timer)
  }, [deadline])

  const start = useCallback((seconds: number) => {
    const current = Date.now()
    setNow(current)
    setDeadline(current + seconds * 1000)
  }, [])

  const remaining = deadline === null ? 0 : Math.max(0, Math.ceil((deadline - now) / 1000))
  return { remaining, start }
}
