import { useCallback, useSyncExternalStore } from 'react'

export type Theme = 'light' | 'dark'

const storageKey = 'tocatdz-theme'
const root = document.documentElement

// Tema awal sudah dipasang oleh public/theme.js sebelum React dimuat; di sini hanya dibaca dan diubah.
function current(): Theme {
  return root.getAttribute('data-bs-theme') === 'dark' ? 'dark' : 'light'
}

function subscribe(callback: () => void) {
  const observer = new MutationObserver(callback)
  observer.observe(root, { attributes: true, attributeFilter: ['data-bs-theme'] })
  return () => observer.disconnect()
}

export function useTheme() {
  const theme = useSyncExternalStore(subscribe, current, () => 'light' as Theme)
  const toggle = useCallback(() => {
    const next: Theme = current() === 'dark' ? 'light' : 'dark'
    root.setAttribute('data-bs-theme', next)
    try {
      window.localStorage.setItem(storageKey, next)
    } catch {
      // Penyimpanan diblokir browser; tema tetap berlaku sampai halaman dimuat ulang.
    }
  }, [])
  return { theme, toggle }
}
