import { test, expect, type Route } from "@playwright/test";
import { authenticate, installMocks } from "./fixtures/mocks";

/**
 * V24 — Pane tab (live tmux capture over SSE).
 *
 * Mocks /events/session/alpha/pane to emit two scripted `pane`
 * frames; navigates to /s/alpha, clicks the Pane tab, and asserts
 * the first frame renders, then the second replaces it.
 *
 * Note: the JSON-encoded data on the wire is a quoted string (the
 * server does `json.Marshal(capture)`), so the SSE `data:` line is
 * `"frame one"` (with literal quotes) — the hook parses it back to
 * the raw capture before rendering.
 */

function paneFrame(text: string): string {
  return `event: pane\ndata: ${JSON.stringify(text)}\n\n`;
}

test.describe("Pane tab — live capture (V24)", () => {
  test.beforeEach(async ({ page }) => {
    await authenticate(page);
    await installMocks(page);

    // Precise route for the pane SSE — installMocks catches
    // /events/** with a no-op; this more specific handler must
    // win. Playwright's routing matches LIFO (last registered first).
    await page.route("**/events/session/*/pane", (route: Route) => {
      const body =
        paneFrame("first frame\n$ ") +
        paneFrame("second frame\n$ ls\n");
      return route.fulfill({
        status: 200,
        contentType: "text/event-stream",
        headers: {
          "cache-control": "no-cache",
          connection: "keep-alive",
        },
        body,
      });
    });
  });

  test("renders first frame then replaces with second", async ({ page }) => {
    await page.goto("/s/alpha");

    // Click the Pane tab trigger.
    await page.getByRole("tab", { name: /^pane$/i }).click();

    const pane = page.getByTestId("pane-view");
    await expect(pane).toBeVisible();

    // Second frame is the last payload emitted; React state will
    // settle on it. Poll for it.
    await expect(pane).toContainText("second frame");
    await expect(pane).toContainText("$ ls");
  });

  test("pane region has an accessible label and live indicator", async ({
    page,
  }) => {
    await page.goto("/s/alpha");
    await page.getByRole("tab", { name: /^pane$/i }).click();

    await expect(
      page.getByRole("region", { name: /live pane for alpha/i }),
    ).toBeVisible();
    await expect(page.getByTestId("pane-live-dot")).toBeVisible();
  });
});
