import '@testing-library/jest-dom/vitest'
import { cleanup, configure, render } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { MemoryRouter, type InitialEntry } from 'react-router-dom'
import { afterEach, vi } from 'vitest'
import { SessionProvider } from '../auth/SessionProvider.tsx'

// Node menolak URL relatif sedangkan client memakai /api/v1 sehingga base dari jsdom ditambahkan
const nativeFetch = globalThis.fetch
vi.stubGlobal('fetch', (input: RequestInfo | URL, init?: RequestInit) => nativeFetch(typeof input === 'string' && input.startsWith('/') ? new URL(input, window.location.href) : input, init))

// Test pertama di tiap file memuat seluruh aplikasi sehingga batas tunggu bawaan 1 detik terlalu pendek saat banyak file berjalan paralel
configure({ asyncUtilTimeout: 4000 })

// Layout aplikasi membaca media query sedangkan jsdom tidak menyediakannya sehingga selalu dianggap layar desktop
vi.stubGlobal('matchMedia', (query: string) => ({ matches: false, media: query, addEventListener: () => undefined, removeEventListener: () => undefined }))

// CodeMirror mengukur teks lewat geometri Range yang tidak disediakan jsdom sehingga dibuat kosong
const emptyRects = Object.assign([], { item: () => null })
Range.prototype.getClientRects = () => emptyRects as unknown as DOMRectList
Range.prototype.getBoundingClientRect = () => new DOMRect(0, 0, 0, 0)

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  window.localStorage.clear()
  document.documentElement.removeAttribute('data-bs-theme')
})

// Render dengan provider yang sama seperti aplikasi, tanpa retry supaya kegagalan langsung terlihat
export function renderApp(ui: ReactNode, route: InitialEntry = '/') {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false }, mutations: { retry: false } } })
  const view = render(
    <QueryClientProvider client={queryClient}>
      <MemoryRouter initialEntries={[route]}>
        <SessionProvider>{ui}</SessionProvider>
      </MemoryRouter>
    </QueryClientProvider>,
  )
  return { queryClient, ...view }
}
