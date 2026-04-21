import { test, expect, type Route } from "@playwright/test";
import { authenticate, installMocks } from "./fixtures/mocks";

/**
 * V10 — Feed tab "All | Bash" filter.
 *
 * The SPA populates its feed cache from SSE (/events/all), not from
 * /api/feed (the REST endpoint is kept only for scripting / external
 * consumers). So we mock /events/all with a stream that emits a mix of
 * Bash, Edit, and Read tool_call events, then assert the filter toggle
 * shows them all initially and collapses to Bash-only on click.
 *
 * We also fulfill /api/feed for good measure so any consumer that still
 * reads it gets the same dataset.
 */

const toolCalls = [
  {
    type: "tool_call",
    session: "alpha",
    tool: "Bash",
    input: "ls -la /tmp",
    summary: "total 0",
    is_error: false,
    ts: "2026-04-21T16:28:00Z",
  },
  {
    type: "tool_call",
    session: "alpha",
    tool: "Edit",
    input: "src/foo.ts",
    summary: "1 line changed",
    is_error: false,
    ts: "2026-04-21T16:28:05Z",
  },
  {
    type: "tool_call",
    session: "alpha",
    tool: "Bash",
    input: "echo hi && false",
    summary: "hi",
    is_error: true,
    exit_code: 1,
    ts: "2026-04-21T16:28:10Z",
  },
  {
    type: "tool_call",
    session: "alpha",
    tool: "Read",
    input: "/etc/hosts",
    summary: "127.0.0.1 localhost",
    is_error: false,
    ts: "2026-04-21T16:28:15Z",
  },
];

function sseBody(
  events: Array<{ type: string } & Record<string, unknown>>,
): string {
  return (
    events
      .map((e) => {
        // Fan the event out as its named SSE type so SseProvider's
        // switch(ev.type) dispatches it as a tool_call. The `type`
        // field is also embedded in the JSON payload for parity
        // with how the Go hub encodes events.
        return `event: ${e.type}\ndata: ${JSON.stringify(e)}\n\n`;
      })
      .join("") + ": keepalive\n\n"
  );
}

test.describe("Feed tab — Bash filter (V10)", () => {
  test.beforeEach(async ({ page }) => {
    await authenticate(page);
    await installMocks(page, { feed: toolCalls });

    // Override the default no-op /events route with a stream that
    // ships the seeded tool_call events — ONCE. Subsequent reconnect
    // attempts (fetch-event-source auto-retries when the response
    // body ends) land on the fall-through no-op stream so we don't
    // double-append into the feed cache.
    await page.route(
      "**/events/**",
      (route: Route) => {
        return route.fulfill({
          status: 200,
          contentType: "text/event-stream",
          headers: {
            "cache-control": "no-cache",
            connection: "keep-alive",
          },
          body: sseBody(toolCalls),
        });
      },
      { times: 1 },
    );
  });

  test("shows all tool calls under All and only Bash under Bash", async ({
    page,
  }) => {
    await page.goto("/s/alpha");

    const feed = page.getByRole("region", { name: /feed for alpha/i });
    await expect(feed).toBeVisible();

    // All: expect all three tool names to appear somewhere in the feed.
    await expect(feed.getByText("Bash").first()).toBeVisible();
    await expect(feed.getByText("Edit").first()).toBeVisible();
    await expect(feed.getByText("Read").first()).toBeVisible();

    // Click Bash filter.
    await page.getByRole("tab", { name: /^bash$/i }).click();

    // Now the compact BashOnlyRow strip is the renderer: rows expose
    // a data-testid="bash-row". Expect exactly 2 (two Bash events).
    await expect(page.locator('[data-testid="bash-row"]')).toHaveCount(2);

    // Neither Edit nor Read command text is visible.
    await expect(feed.getByText("src/foo.ts")).toHaveCount(0);
    await expect(feed.getByText("/etc/hosts")).toHaveCount(0);

    // One ok chip (ls -la) and one err chip (echo hi && false).
    await expect(
      page.locator('[data-testid="bash-chip"][data-status="ok"]'),
    ).toHaveCount(1);
    await expect(
      page.locator('[data-testid="bash-chip"][data-status="err"]'),
    ).toHaveCount(1);
  });

  test("persists the Bash selection across reload via sessionStorage", async ({
    page,
  }) => {
    await page.goto("/s/alpha");
    await page.getByRole("tab", { name: /^bash$/i }).click();
    await expect(
      page.getByRole("tab", { name: /^bash$/i }),
    ).toHaveAttribute("aria-selected", "true");

    await page.reload();

    // On reload, the Bash filter should still be active.
    await expect(
      page.getByRole("tab", { name: /^bash$/i }),
    ).toHaveAttribute("aria-selected", "true");
  });
});
