import { useEffect, useState } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'

const isObject = (value: unknown): value is Record<string, unknown> => typeof value === 'object' && value !== null

// Pesan sekali tampil yang dibawa lewat state router setelah berpindah halaman, misalnya setelah draft berhasil dibuat
export function readNotice(state: unknown): string | null {
  return isObject(state) && typeof state.notice === 'string' ? state.notice : null
}

// Pesan dibaca sekali lalu dihapus dari state riwayat supaya tidak muncul lagi saat halaman dimuat ulang
export function useFlashNotice() {
  const location = useLocation()
  const navigate = useNavigate()
  const [notice, setNotice] = useState<string | null>(() => readNotice(location.state))
  useEffect(() => {
    if (readNotice(location.state)) void navigate(`${location.pathname}${location.search}`, { replace: true, state: null })
  }, [location.pathname, location.search, location.state, navigate])
  return [notice, setNotice] as const
}
