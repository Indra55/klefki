import { defineConfig } from '@playwright/test';
export default defineConfig({ testDir: './e2e', workers: 1, timeout: 90000, expect: { timeout: 15000 }, use: { baseURL: 'http://127.0.0.1:5188', acceptDownloads: true }, webServer: { command: 'node scripts/test-server.mjs', url: 'http://127.0.0.1:5188', reuseExistingServer: process.env.VAULT_REUSE_TEST_SERVER === '1', timeout: 120000 } });
