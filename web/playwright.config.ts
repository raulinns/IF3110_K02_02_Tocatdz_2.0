import { defineConfig, devices } from '@playwright/test'

const port = 5200

// Smoke test berjalan terhadap dev:mock memakai Chrome yang sudah terpasang sehingga browser tidak perlu diunduh
export default defineConfig({
  testDir: 'e2e',
  outputDir: 'e2e/.artifacts',
  timeout: 120_000,
  expect: { timeout: 20_000 },
  globalSetup: './e2e/warmup.ts',
  retries: process.env.CI ? 1 : 0,
  workers: process.env.CI ? 2 : undefined,
  reporter: [['list'], ['html', { open: 'never', outputFolder: 'e2e/.report' }]],
  use: {
    baseURL: `http://localhost:${port}`,
    channel: 'chrome',
    locale: 'id-ID',
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
  },
  projects: [
    { name: 'desktop', use: { ...devices['Desktop Chrome'], channel: 'chrome', viewport: { width: 1280, height: 800 } } },
    { name: 'mobile', use: { ...devices['Pixel 7'], channel: 'chrome', viewport: { width: 390, height: 844 } } },
  ],
  webServer: {
    command: `npm run dev:mock -- --port ${port} --strictPort`,
    url: `http://localhost:${port}`,
    reuseExistingServer: !process.env.CI,
    timeout: 120_000,
  },
})
