import { test, expect, type Route } from "@playwright/test";
import { authenticate, installMocks } from "./fixtures/mocks";

/**
 * V9 — expandable inline diff viewer on Edit/Write/MultiEdit feed
 * rows.
 *
 * The SPA hydrates its feed from SSE (/events/all), stamping each
 * hub Event.ID onto the resulting ToolCallRow in the cache. When the
 * user clicks the expand chevron, ToolCallRow fires
 * /api/sessions/:name/tool_calls/:id/detail and renders the returned
 * unified diff with +/-/@@  colouring.
 *
 * We seed one Edit call, mock the detail endpoint once, and assert
 * the diff surfaces with the correct colour tokens.
 */

const EVENT_ID = "17771234000000000-0";

const EDIT_CALL = {
  type: "tool_call",
  session: "alpha",
  tool: "Edit",
  input: "src/foo.ts",
  summary: "1 line changed",
  is_error: false,
  ts: "2026-04-21T16:28:05Z",
};

const SAMPLE_DETAIL = {
  tool: "Edit",
  input_json: JSON.stringify({
    file_path: "src/foo.ts",
    old_string: "foo",
    new_string: "bar",
  }),
  output_excerpt: "",
  ts: EDIT_CALL.ts,
  is_error: false,
  diff: [
    "--- a/src/foo.ts",
    "+++ b/src/foo.ts",
    "@@ -1,1 +1,1 @@",
    "-foo",
    "+bar",
  ].join("\n"),
};

function sseBody(): string {
  // `id:` on the SSE message line is what fetch-event-source surfaces
  // as EventSourceMessage.id — SseProvider stamps that onto the
  // ToolCallRow in cache so ToolCallRow can request detail by id.
  return (
    `event: tool_call\nid: ${EVENT_ID}\ndata: ${JSON.stringify(EDIT_CALL)}\n\n` +
    `: keepalive\n\n`
  );
}

test.describe("Feed row — inline diff detail (V9)", () => {
  test.beforeEach(async ({ page }) => {
    await authenticate(page);
    await installMocks(page, { feed: [EDIT_CALL] });

    // Serve the seeded Edit event over SSE exactly once; further
    // reconnects fall through to the no-op stream from installMocks
    // so the feed cache doesn't double-append.
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
          body: sseBody(),
        });
      },
      { times: 1 },
    );

    // Detail endpoint — fulfilled once per test, asserting the id
    // round-trips intact.
    await page.route(
      `**/api/sessions/alpha/tool_calls/${EVENT_ID}/detail`,
      (route: Route) =>
        route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify(SAMPLE_DETAIL),
        }),
    );
  });

  test("expanding an Edit row shows a coloured unified diff", async ({
    page,
  }) => {
    await page.goto("/s/alpha");

    const feed = page.getByRole("region", { name: /feed for alpha/i });
    await expect(feed).toBeVisible();

    // Wait for the Edit row to surface via SSE.
    const editRow = feed.locator("article", { hasText: "Edit" }).first();
    await expect(editRow).toBeVisible();

    // Expand control is present for Edit/MultiEdit/Write only.
    const expand = editRow.getByTestId("tool-expand");
    await expect(expand).toBeVisible();
    await expect(expand).toHaveAttribute("aria-expanded", "false");

    await expand.click();
    await expect(expand).toHaveAttribute("aria-expanded", "true");

    // Diff renders.
    const diff = editRow.getByTestId("tool-diff");
    await expect(diff).toBeVisible();

    // Added line → emerald.
    const added = diff.getByText("+bar");
    await expect(added).toBeVisible();
    await expect(added).toHaveClass(/text-emerald-400/);

    // Removed line → alert-ember.
    const removed = diff.getByText("-foo");
    await expect(removed).toBeVisible();
    await expect(removed).toHaveClass(/text-alert-ember/);

    // Hunk header → fg-dim.
    const hunk = diff.getByText("@@ -1,1 +1,1 @@");
    await expect(hunk).toBeVisible();
    await expect(hunk).toHaveClass(/text-fg-dim/);
  });
});
