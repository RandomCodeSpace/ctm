/// <reference types="node" />
import { defineConfig, devices } from "@playwright/test";

/**
 * Playwright E2E config. Drives the React SPA against a mocked /api + /events
 * surface via `page.route` so tests stay fast and deterministic; backed by the
 * vite preview server (built bundle, not dev server) so CSS/asset resolution
 * matches production.
 */
export default defineConfig({
  testDir: "./e2e",
  outputDir: "./test-results",
  timeout: 15_000,
  expect: { timeout: 3_000 },
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  reporter: process.env.CI ? "github" : "list",

  use: {
    baseURL: "http://127.0.0.1:4173",
    trace: "retain-on-failure",
    screenshot: "only-on-failure",
  },

  projects: [
    {
      name: "chromium",
      use: { ...devices["Desktop Chrome"] },
    },
  ],

  // webServer assumes `dist/` is already built. `make e2e` runs
  // `pnpm build` first; running `playwright test` directly also works
  // if you've recently built. Vite preview serves the static bundle —
  // no dev-server HMR noise — matching what ships to users.
  webServer: {
    command:
      "pnpm exec vite preview --port 4173 --strictPort --host 127.0.0.1",
    url: "http://127.0.0.1:4173",
    reuseExistingServer: !process.env.CI,
    timeout: 30_000,
    stdout: "ignore",
    stderr: "pipe",
  },
});
