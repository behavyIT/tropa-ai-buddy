import { defineConfig } from '@playwright/test'

/**
 * E2E-прогон основного сценария по реальному стенду с записью видео.
 * Требует запущенный стенд (docker compose up) — по умолчанию http://127.0.0.1:8080.
 */
export default defineConfig({
  testDir: 'e2e',
  timeout: 10 * 60 * 1000,
  expect: { timeout: 15000 },
  outputDir: 'test-results',
  reporter: [['list']],
  use: {
    baseURL: process.env.E2E_BASE_URL ?? 'http://127.0.0.1:8080',
    channel: process.env.E2E_CHANNEL ?? 'chrome',
    viewport: { width: 1440, height: 900 },
    // видео пишется через CDP screencast (e2e/screencast.ts) и собирается системным ffmpeg
    video: 'off',
    launchOptions: { slowMo: 40 },
    locale: 'ru-RU',
    timezoneId: 'Europe/Moscow',
  },
})
