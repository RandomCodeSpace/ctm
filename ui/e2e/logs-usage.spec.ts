import { test, expect, type Route } from "@playwright/test";
import { authenticate, installMocks, defaultSession } from "./fixtures/mocks";

const LOGS_USAGE_PAYLOAD = {
  dir: "/home/dev/.config/ctm/logs",
  total_bytes: 1024 * 1024 * 3, // 3 MB
  files: [
    {
      uuid: "00000000-0000-0000-0000-000000000001",
      session: "alpha",
      bytes: 1024 * 1024 * 2, // 2 MB
      mtime: new Date(Date.now() - 5 * 60_000).toISOString(),
    },
    {
      uuid: "22222222-0000-0000-0000-000000000002",
      session: "beta",
      bytes: 1024 * 512, // 512 KB
      mtime: new Date(Date.now() - 30 * 60_000).toISOString(),
    },
    {
      uuid: "33333333-0000-0000-0000-000000000003",
      session: "uuid:33333333",
      bytes: 1024 * 512, // 512 KB
      mtime: new Date(Date.now() - 2 * 3600_000).toISOString(),
    },
  ],
};

test.describe("Log disk usage (Meta tab)", () => {
  test.beforeEach(async ({ page }) => {
    await authenticate(page);
    await installMocks(page);

    // Per-session Get endpoint — dashboard sessions list already covered
    // by installMocks, but SessionDetail fires its own /api/sessions/{name}.
    await page.route("**/api/sessions/**", (route: Route) => {
      const url = new URL(route.request().url());
      // Skip routes already handled by installMocks (list + feed).
      if (
        url.pathname.endsWith("/feed") ||
        url.pathname === "/api/sessions"
      ) {
        return route.fallback();
      }
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify(defaultSession),
      });
    });

    await page.route("**/api/logs/usage", (route: Route) => {
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify(LOGS_USAGE_PAYLOAD),
      });
    });
  });

  test("renders total + per-session rows on the Meta tab", async ({ page }) => {
    await page.goto("/s/alpha/meta");

    const region = page.getByRole("region", { name: /log disk usage/i });
    await expect(region).toBeVisible();

    // Total in the header — 3 MB.
    await expect(region).toContainText("3 MB");

    // Per-session rows visible with humanised sizes.
    await expect(region.getByText("alpha")).toBeVisible();
    await expect(region.getByText("beta")).toBeVisible();
    await expect(region.getByText("uuid:33333333")).toBeVisible();
    await expect(region.getByText("2 MB")).toBeVisible();
    // Two files at 512 KB — use locator count rather than toBeVisible
    // because both render identically.
    await expect(region.getByText("512 KB")).toHaveCount(2);

    // Dir path surfaces in the footer.
    await expect(region.getByText("/home/dev/.config/ctm/logs")).toBeVisible();
  });
});
