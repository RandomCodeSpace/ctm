# Playwright E2E — ctm serve UI

Fast, deterministic browser tests for the React SPA. Mocks `/api/**` and
`/events/**` per-page so the suite never needs a running daemon or fixture
DB, and runs against a production vite-preview bundle so CSS and asset
resolution match what ships to users.

## Running

```sh
make e2e                       # from repo root — rebuilds bundle + runs suite
pnpm exec playwright test      # from ui/ — assumes dist/ is already built
make regression                # runs this suite plus Go/vitest/audit
```

One-time setup on a fresh clone:
```sh
pnpm --prefix ui install
pnpm --prefix ui exec playwright install chromium
```

## Growing the pack

**Every shipped bug fix or feature adds a test here that would have caught
it.** The pack is append-only — do not replace existing specs when you
extend them. Name new spec files after the feature or regression ID
(`quota-strip.spec.ts`, `auth.spec.ts`, …) and group related cases inside
a single `test.describe` block.

## Mocks

`e2e/fixtures/mocks.ts` exposes `installMocks(page, overrides?)` and
`authenticate(page)`. Defaults return a single happy-path session, a
quota snapshot with future reset times, an empty feed, and a held-open
SSE stream. Override per-case:

```ts
await installMocks(page, {
  sessions: [{ ...alpha, is_active: false }],
  authenticated: false,  // forces /api/bootstrap → 401
});
```

Shapes mirror the real API (`/api/sessions` → `Session[]`, `/api/feed` →
`ToolCallRow[]`) — if you change either contract in `internal/serve/api`,
update the fixture too.

## Debugging

```sh
pnpm exec playwright test --debug             # launch inspector
pnpm exec playwright test auth.spec.ts --ui   # interactive UI mode
playwright show-trace test-results/<name>/trace.zip
```

Traces are retained only on failure (see `playwright.config.ts`).
