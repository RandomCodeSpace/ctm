import { test, expect, type Route } from "@playwright/test";
import { authenticate, installMocks } from "./fixtures/mocks";

/**
 * V6 — "Load older" button fetches from /api/sessions/{name}/feed/history
 * when the user wants to scroll past the 500-slot ring buffer.
 *
 * We seed the live feed (via SSE, as bash-filter.spec does) with two
 * recent tool calls, then mock the history endpoint to return 5
 * older tool calls. Clicking "Load older" must append them below the
 * live rows and hide the button when has_more is false.
 */

const liveEvents = [
  {
    type: "tool_call",
    session: "alpha",
    tool: "Bash",
    input: "live-command-a",
    summary: "ok",
    is_error: false,
    ts: "2026-04-21T16:30:00Z",
  },
  {
    type: "tool_call",
    session: "alpha",
    tool: "Edit",
    input: "src/live.ts",
    summary: "1 line changed",
    is_error: false,
    ts: "2026-04-21T16:30:05Z",
  },
];

const historyPayload = {
  events: Array.from({ length: 5 }).map((_, i) => ({
    id: `${100 + i}-0`,
    session: "alpha",
    type: "tool_call",
    ts: `2026-04-21T14:00:0${i}Z`,
    payload: {
      session: "alpha",
      tool: "Read",
      input: `older-file-${i}.txt`,
      summary: "read 10 lines",
      is_error: false,
      ts: `2026-04-21T14:00:0${i}Z`,
    },
  })),
  has_more: false,
};

function sseBody(
  events: Array<{ type: string } & Record<string, unknown>>,
): string {
  return (
    events
      .map((e) => `event: ${e.type}\ndata: ${JSON.stringify(e)}\n\n`)
      .join("") + ": keepalive\n\n"
  );
}

test.describe("Feed history — Load older (V6)", () => {
  test.beforeEach(async ({ page }) => {
    await authenticate(page);
    await installMocks(page);

    // Feed the live SSE stream once; subsequent reconnects drop through
    // to the installMocks no-op stream.
    await page.route(
      "**/events/**",
      (route: Route) =>
        route.fulfill({
          status: 200,
          contentType: "text/event-stream",
          headers: {
            "cache-control": "no-cache",
            connection: "keep-alive",
          },
          body: sseBody(liveEvents),
        }),
      { times: 1 },
    );

    // History endpoint — match the full feed/history path so the
    // broader /api/sessions/** mock in installMocks doesn't swallow it.
    await page.route("**/api/sessions/alpha/feed/history**", (route: Route) =>
      route.fulfill({
        contentType: "application/json",
        body: JSON.stringify(historyPayload),
      }),
    );
  });

  test("clicking Load older appends historical rows and hides the button", async ({
    page,
  }) => {
    await page.goto("/s/alpha");

    const feed = page.getByRole("region", { name: /feed for alpha/i });
    await expect(feed).toBeVisible();

    // Wait for the button to appear; its visibility depends on the
    // feed cache being populated (rows.length > 0). Checking the
    // button directly avoids racing against a transient empty-cache
    // render under parallel-worker load.
    const button = page.getByRole("button", { name: /load older/i });
    await expect(button).toHaveCount(1, { timeout: 10_000 });

    // Live rows are present before click.
    await expect(feed.getByText("live-command-a")).toBeVisible();
    await expect(feed.getByText("src/live.ts")).toBeVisible();
    await expect(feed.getByText("older-file-0.txt")).toHaveCount(0);

    await button.scrollIntoViewIfNeeded();
    await button.click();

    // All five historical entries appended.
    for (let i = 0; i < 5; i++) {
      await expect(feed.getByText(`older-file-${i}.txt`)).toBeVisible();
    }

    // has_more: false in the mocked response → button hidden.
    await expect(
      page.getByRole("button", { name: /load older/i }),
    ).toHaveCount(0);
  });
});
