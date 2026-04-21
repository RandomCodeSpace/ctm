import type { Page, Route } from "@playwright/test";

/**
 * Default mock surface: one happy-path session, one quota snapshot,
 * no attention alerts, empty feed. Tests override per-case by calling
 * `installMocks(page, { overrides })` or by adding their own route()
 * handlers after.
 */
export interface MockOverrides {
  bootstrap?: unknown;
  sessions?: unknown;
  quota?: unknown;
  feed?: unknown;
  /** When false, /api/bootstrap returns 401 so the paste screen renders. */
  authenticated?: boolean;
}

const defaultSession = {
  name: "alpha",
  uuid: "00000000-0000-0000-0000-000000000001",
  mode: "safe",
  workdir: "/home/dev/projects/ctm",
  created_at: "2026-04-21T15:00:00Z",
  last_attached_at: "2026-04-21T16:00:00Z",
  last_tool_call_at: "2026-04-21T16:28:00Z",
  is_active: true,
  tmux_alive: true,
  context_pct: 42,
  tokens: { input_tokens: 12_340, output_tokens: 2_100, cache_tokens: 55_000 },
};

const defaultQuota = {
  weekly_pct: 48,
  five_hr_pct: 24,
  // A reset 3 hours into the future — verifies the relativeFuture helper.
  weekly_resets_at: new Date(Date.now() + 3 * 3600_000).toISOString(),
  five_hr_resets_at: new Date(Date.now() + 45 * 60_000).toISOString(),
};

export async function installMocks(
  page: Page,
  overrides: MockOverrides = {},
): Promise<void> {
  const authed = overrides.authenticated !== false;

  await page.route("**/api/bootstrap", (route: Route) => {
    if (!authed) return route.fulfill({ status: 401 });
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify(
        overrides.bootstrap ?? {
          version: "e2e-test",
          port: 37778,
          has_webhook: false,
        },
      ),
    });
  });

  await page.route("**/api/sessions**", (route: Route) => {
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify(overrides.sessions ?? [defaultSession]),
    });
  });

  await page.route("**/api/quota", (route: Route) => {
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify(overrides.quota ?? defaultQuota),
    });
  });

  await page.route("**/api/feed**", (route: Route) => {
    return route.fulfill({
      contentType: "application/json",
      body: JSON.stringify(overrides.feed ?? []),
    });
  });

  // Keep SSE calls quiet — route with a held-open no-op stream.
  await page.route("**/events/**", (route: Route) => {
    return route.fulfill({
      status: 200,
      contentType: "text/event-stream",
      body: ": ok\n\n",
    });
  });
}

/** Store an auth token in localStorage so the SPA skips the paste screen. */
export async function authenticate(page: Page): Promise<void> {
  await page.addInitScript(() => {
    window.localStorage.setItem("ctm.token", "e2e-test-token");
  });
}

export { defaultSession, defaultQuota };
