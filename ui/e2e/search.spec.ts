import { test, expect } from "@playwright/test";
import { authenticate, installMocks } from "./fixtures/mocks";

/**
 * V19 Slice 2 — SearchPalette e2e. Routes /api/search to a fixed
 * three-match payload so we can exercise the full input-to-navigation
 * flow with predictable snippets.
 */
test.describe("SearchPalette", () => {
  const fixtureMatches = [
    {
      session: "alpha",
      uuid: "u-1",
      ts: "2026-04-21T16:00:00Z",
      tool: "Bash",
      snippet: "first needle in the haystack",
    },
    {
      session: "alpha",
      uuid: "u-2",
      ts: "2026-04-21T16:05:00Z",
      tool: "Edit",
      snippet: "second needle row",
    },
    {
      session: "beta",
      uuid: "u-3",
      ts: "2026-04-21T16:10:00Z",
      tool: "Read",
      snippet: "third needle file",
    },
  ];

  test.beforeEach(async ({ page }) => {
    await authenticate(page);
    await installMocks(page);
    await page.route("**/api/search**", (route) =>
      route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({
          query: "needle",
          matches: fixtureMatches,
          scanned_files: 5,
          truncated: false,
        }),
      }),
    );
  });

  test("Cmd+K opens the palette and shows 3 mocked results", async ({
    page,
  }) => {
    await page.goto("/");

    // Open via the hotkey — works on both macOS and Linux CI via Meta.
    await page.keyboard.press("Meta+KeyK");

    const input = page.getByRole("combobox");
    await expect(input).toBeVisible();
    await input.fill("needle");

    // Three rows visible in the listbox.
    const options = page.getByRole("option");
    await expect(options).toHaveCount(3);
  });

  test("clicking the first row navigates to /s/alpha", async ({ page }) => {
    await page.goto("/");
    await page.keyboard.press("Meta+KeyK");

    const input = page.getByRole("combobox");
    await input.fill("needle");

    const firstOption = page.getByRole("option").first();
    await expect(firstOption).toBeVisible();
    await firstOption.click();

    await expect(page).toHaveURL(/\/s\/alpha(\/|$)/);
  });
});
