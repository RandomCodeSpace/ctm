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

  // Catch-all for unmocked /api/** endpoints. Registered first so the
  // specific mocks below override it (Playwright matches the most
  // recently registered route). Returns a safe empty payload shaped by
  // route — crucially NOT 401 — because any 401 bubbles up as
  // UnauthorizedError and triggers AuthProvider.signOut(), which clears
  // the bearer token and boots the test back to the paste screen. That
  // masks every subsequent assertion and makes the suite extremely
  // brittle to new endpoints (e.g. V13's /api/cost was silently dropping
  // all tests into the paste screen until this catch-all was added).
  await page.route("**/api/**", (route: Route) => {
    const url = route.request().url();
    const path = new URL(url).pathname;
    let body = "{}";
    if (path.startsWith("/api/cost")) {
      body = JSON.stringify({
        window: "day",
        points: [],
        totals: { input: 0, output: 0, cache: 0, cost_usd_micros: 0 },
      });
    } else if (path.startsWith("/api/search")) {
      body = JSON.stringify({ query: "", matches: [], truncated: false });
    } else if (path.endsWith("/subagents")) {
      body = JSON.stringify({ subagents: [] });
    } else if (path.endsWith("/teams")) {
      body = JSON.stringify({ teams: [] });
    } else if (path.endsWith("/checkpoints")) {
      body = "[]";
    } else if (path.endsWith("/feed/history")) {
      body = JSON.stringify({ events: [], has_more: false });
    } else if (path === "/api/logs/usage") {
      body = JSON.stringify({ dir: "", total_bytes: 0, files: [] });
    } else if (path === "/api/doctor") {
      body = JSON.stringify({ checks: [], ok: true });
    } else if (path === "/api/config") {
      body = "{}";
    } else if (/\/api\/sessions\/[^/]+\/input$/.test(path)) {
      // V25 session-input default: 204 No Content. Tests override with
      // page.route(...) when they want an error case.
      return route.fulfill({ status: 204 });
    }
    return route.fulfill({ contentType: "application/json", body });
  });

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
    const path = new URL(route.request().url()).pathname;
    // Differentiate the list endpoint from the per-session detail
    // endpoint. Both share the `/api/sessions` prefix so a single
    // glob-mock would otherwise return the list payload for
    // `/api/sessions/alpha` — leaving useSessionDetail with an array
    // where it expects a Session object, and crashing MetaList /
    // CostChart on undefined dates. Nested paths like
    // `/api/sessions/alpha/checkpoints` fall through to the
    // shape-aware catch-all above.
    if (path === "/api/sessions") {
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify(overrides.sessions ?? [defaultSession]),
      });
    }
    const m = path.match(/^\/api\/sessions\/([^/]+)$/);
    if (m) {
      const name = decodeURIComponent(m[1]);
      const sessions = (overrides.sessions ?? [defaultSession]) as Array<{
        name: string;
      }>;
      const found = sessions.find((s) => s.name === name) ?? defaultSession;
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify(found),
      });
    }
    return route.fallback();
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
