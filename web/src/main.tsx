import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter } from 'react-router-dom'
import 'bootstrap/dist/css/bootstrap.min.css'
import '@fortawesome/fontawesome-free/css/fontawesome.min.css'
import '@fortawesome/fontawesome-free/css/solid.min.css'
import './styles/app.css'
import './styles/shell.css'
import App from './App.tsx'
import { shouldRetryQuery } from './api/retry.ts'
import { SessionProvider } from './auth/SessionProvider.tsx'

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      retry: shouldRetryQuery,
      refetchOnWindowFocus: false,
    },
    mutations: { retry: false },
  },
})

// Mode mock menyalakan API palsu lebih dulu dan blok ini dibuang dari build produksi
async function enableMocking() {
  if (import.meta.env.MODE === 'mock') {
    const { startMocking } = await import('./mocks/browser.ts')
    await startMocking()
  }
}

void enableMocking().then(() => {
  createRoot(document.getElementById('root')!).render(
    <StrictMode>
      <QueryClientProvider client={queryClient}>
        <BrowserRouter>
          <SessionProvider>
            <App />
          </SessionProvider>
        </BrowserRouter>
      </QueryClientProvider>
    </StrictMode>,
  )
})
