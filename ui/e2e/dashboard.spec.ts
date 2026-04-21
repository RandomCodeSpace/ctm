import { test, expect } from "@playwright/test";
import { authenticate, installMocks } from "./fixtures/mocks";

test.describe("Dashboard", () => {
  test.beforeEach(async ({ page }) => {
    await authenticate(page);
    await installMocks(page);
  });

  test("renders the seeded session card", async ({ page }) => {
    await page.goto("/");
    // Desktop auto-navigates to the top session so "alpha" appears in
    // both the list card and detail heading — assert on the link card.
    await expect(
      page.getByRole("link", { name: /alpha/i }).first(),
    ).toBeVisible();
    await expect(page.getByText(/safe/i).first()).toBeVisible();
  });

  test("shows last tool call time on the card", async ({ page }) => {
    // Seeded tool call is at 16:28; attached at 16:00. Card should carry
    // both time readings (⏵ for tool call, plain for attached).
    await page.goto("/");
    const card = page.getByRole("link", { name: /alpha/i }).first();
    await expect(card).toBeVisible();
    // ⏵ glyph rendered for the tool-call badge.
    await expect(card.locator('time[aria-label="last tool call"]')).toBeVisible();
  });
});
