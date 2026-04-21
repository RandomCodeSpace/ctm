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

  test("renders the stale chip when tool call is older than 30 min", async ({
    page,
  }) => {
    const oldTC = new Date(Date.now() - 45 * 60_000).toISOString();
    await installMocks(page, {
      sessions: [
        {
          name: "dormant",
          uuid: "00000000-0000-0000-0000-00000000dead",
          mode: "safe",
          workdir: "/home/dev/projects/ctm",
          created_at: "2026-04-21T10:00:00Z",
          last_attached_at: "2026-04-21T11:00:00Z",
          last_tool_call_at: oldTC,
          is_active: true,
          tmux_alive: true,
        },
      ],
    });
    await page.goto("/");
    const card = page.getByRole("link", { name: /dormant/i }).first();
    await expect(card.getByLabel("stale session")).toBeVisible();
  });
});
