import { defineConfig } from 'vitest/config'
import react from '@vitejs/plugin-react'
import { msw } from 'msw/vite'

// Dev: proxy /api ke backend Go yang dijalankan dengan PUBLIC_ORIGIN=http://localhost:5173 supaya cek Origin dan CSRF lulus
export default defineConfig(({ mode }) => ({
  // Worker MSW hanya disajikan di mode mock supaya tidak ikut dalam build produksi
  plugins: [react(), mode === 'mock' && msw({ mode: 'worker-only' })],
  server: {
    port: 5173,
    proxy: {
      '/api': { target: 'http://localhost:8080', changeOrigin: false },
      '/healthz': { target: 'http://localhost:8080', changeOrigin: false },
    },
  },
  test: {
    include: ['src/**/*.test.{ts,tsx}'],
    // Test komponen memuat aplikasi penuh sehingga batas 5 detik bawaan bisa terlewati saat beban tinggi
    testTimeout: 10_000,
  },
}))
