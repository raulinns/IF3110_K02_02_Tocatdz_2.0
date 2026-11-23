import '@testing-library/jest-dom/vitest'
import { cleanup, render } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { MemoryRouter, type InitialEntry } from 'react-router-dom'
import { afterEach, vi } from 'vitest'
import { SessionProvider } from '../auth/SessionProvider.tsx'

// Node menolak URL relatif sedangkan client memakai /api/v1 sehingga base dari jsdom ditambahkan
const nativeFetch = globalThis.fetch
vi.stubGlobal('fetch', (input: RequestInfo | URL, init?: RequestInit) => nativeFetch(typeof input === 'string' && input.startsWith('/') ? new URL(input, window.location.href) : input, init))

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
