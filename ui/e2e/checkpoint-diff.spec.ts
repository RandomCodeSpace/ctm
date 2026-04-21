import { test, expect, type Route } from "@playwright/test";
import { authenticate, installMocks } from "./fixtures/mocks";

const FULL_SHA = "abcdef1234567890abcdef1234567890abcdef12";

const SAMPLE_DIFF = [
  "commit abcdef1234567890abcdef1234567890abcdef12",
  "Author: ctm",
  "",
  "diff --git a/foo.go b/foo.go",
  "--- a/foo.go",
  "+++ b/foo.go",
  "@@ -1,3 +1,3 @@",
  " context line",
  "-removed line",
  "+added line",
].join("\n");

test.describe("Checkpoint diff viewer", () => {
  test.beforeEach(async ({ page }) => {
    await authenticate(page);
    await installMocks(page);

    // Checkpoint list: one commit; the "View diff" button's rendered
    // SHA will be derived from this row's `short_sha`.
    await page.route("**/api/sessions/alpha/checkpoints**", (route: Route) =>
      route.fulfill({
        contentType: "application/json",
        body: JSON.stringify([
          {
            sha: FULL_SHA,
            short_sha: FULL_SHA.slice(0, 7),
            subject: "checkpoint: pre-yolo 2026-04-21T12:00:00",
            author: "ctm",
            ts: new Date(Date.now() - 60_000).toISOString(),
          },
        ]),
      }),
    );

    // Diff endpoint. Note the path order differs from /checkpoints so
    // this route won't collide with the list mock above.
    await page.route(
      `**/api/sessions/alpha/checkpoints/${FULL_SHA}/diff`,
      (route: Route) =>
        route.fulfill({
          status: 200,
          contentType: "text/plain",
          body: SAMPLE_DIFF,
        }),
    );
  });

  test("View diff button opens the sheet with coloured lines", async ({
    page,
  }) => {
    await page.goto("/s/alpha/checkpoints");

    // Row is present, then click the sibling "View diff" action.
    const viewDiff = page.getByRole("button", { name: /view diff/i });
    await expect(viewDiff).toBeVisible();
    await viewDiff.click();

    // Sheet surfaces.
    await expect(
      page.getByRole("heading", { name: /checkpoint diff/i }),
    ).toBeVisible();

    // Added line → emerald.
    const added = page.getByText("+added line");
    await expect(added).toBeVisible();
    await expect(added).toHaveClass(/text-emerald-400/);

    // Removed line → alert-ember.
    const removed = page.getByText("-removed line");
    await expect(removed).toBeVisible();
    await expect(removed).toHaveClass(/text-alert-ember/);

    // Hunk header → fg-dim.
    const hunk = page.getByText("@@ -1,3 +1,3 @@");
    await expect(hunk).toBeVisible();
    await expect(hunk).toHaveClass(/text-fg-dim/);
  });
});
