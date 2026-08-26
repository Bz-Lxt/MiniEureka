import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  testMatch: 'e2e_flow.spec.ts',
  timeout: 30_000,
  expect: { timeout: 8_000 },
  fullyParallel: false,
  forbidOnly: true,
  retries: 0,
  reporter: [['list']],
  use: {
    baseURL: process.env.MINIEUREKA_BASE_URL ?? 'http://127.0.0.1:18780',
    screenshot: 'only-on-failure',
    trace: 'retain-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
})
