import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'

// Dev: proxy /api ke backend Go. Jalankan backend dengan PUBLIC_ORIGIN=http://localhost:5173
// supaya cek Origin/CSRF lulus (Origin diteruskan apa adanya oleh proxy).
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:8080', changeOrigin: false },
      '/healthz': { target: 'http://localhost:8080', changeOrigin: false },
    },
  },
  test: {
    include: ['src/**/*.test.{ts,tsx}'],
  },
})
