const { defineConfig } = require('@playwright/test');

module.exports = defineConfig({
  testDir: '.',
  testMatch: 'dashboard.spec.js',
  fullyParallel: false,
  workers: 1,
  reporter: 'line',
  use: {
    baseURL: 'http://127.0.0.1:17431',
    browserName: 'chromium',
    trace: 'retain-on-failure',
  },
  webServer: {
    command: 'go run .',
    url: 'http://127.0.0.1:17431/_dropserve/api/status',
    reuseExistingServer: false,
    timeout: 120000,
  },
});
