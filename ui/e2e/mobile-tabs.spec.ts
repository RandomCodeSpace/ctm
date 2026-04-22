import { test, expect } from "@playwright/test";
import { authenticate, installMocks } from "./fixtures/mocks";

/**
 * Regression guard: at narrow viewports the SessionDetail tab row holds
 * 6 tabs (Feed, Checkpoints, Subagents, Teams, Pane, Meta) which can't
 * all fit at ~390px. The list must remain horizontally scrollable so
 * Pane and Meta stay reachable — without this guard a future tab add,
 * a CSS refactor, or a `overflow-hidden` slip would silently clip the
 * tail off screen.
 */
test.use({
  viewport: { width: 390, height: 844 },
  isMobile: true,
  hasTouch: true,
});

test.describe("SessionDetail tabs (mobile)", () => {
  test.beforeEach(async ({ page }) => {
    await authenticate(page);
    await installMocks(page);
  });

  test("all six tabs are reachable via horizontal scroll", async ({ page }) => {
    await page.goto("/s/alpha");

    // Scope to the primary SessionDetail tablist — the CostChart on
    // the Meta tab also renders a role=tablist (hour/day/week pills)
    // that would otherwise inflate the count.
    const tablist = page
      .getByRole("tablist")
      .filter({ has: page.getByRole("tab", { name: /feed/i }) })
      .first();
    const tabs = tablist.getByRole("tab");
    await expect(tabs).toHaveCount(6);

    // The last tab (Meta) starts clipped off the right edge but must
    // become clickable after the tablist scrolls it into view. Radix
    // handles keyboard arrow navigation; we drive it via click on the
    // scrolled-into-view element to mirror the touch-scroll flow.
    const metaTab = tablist.getByRole("tab", { name: /meta/i });
    await metaTab.scrollIntoViewIfNeeded();
    await metaTab.click();

    await expect(metaTab).toHaveAttribute("aria-selected", "true");
  });
});
