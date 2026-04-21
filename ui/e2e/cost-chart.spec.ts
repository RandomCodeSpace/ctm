import { test, expect, type Route } from "@playwright/test";
import { authenticate, installMocks, defaultSession } from "./fixtures/mocks";

function makeCostFixture(window: "hour" | "day" | "week" = "day") {
  const now = Date.now();
  const points = Array.from({ length: 10 }).map((_, i) => {
    const ts = new Date(now - (10 - i) * 60_000).toISOString();
    return {
      ts,
      session: "alpha",
      input_tokens: 1000 * (i + 1),
      output_tokens: 500 * (i + 1),
      cache_tokens: 100 * (i + 1),
      cost_usd_micros: 12_000 * (i + 1),
    };
  });
  return {
    window,
    points,
    totals: {
      input: 10_000,
      output: 5_000,
      cache: 1_000,
      cost_usd_micros: 120_000,
    },
  };
}

test.describe("Cumulative cost chart (V13)", () => {
  test.beforeEach(async ({ page }) => {
    await authenticate(page);
    await installMocks(page);

    await page.route("**/api/sessions/**", (route: Route) => {
      const url = new URL(route.request().url());
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

    // Mock /api/cost with a 10-point fixture; honour the window query
    // param so the Hour/Day/Week pill test can assert the client
    // actually re-fetched.
    await page.route("**/api/cost**", (route: Route) => {
      const url = new URL(route.request().url());
      const window = (url.searchParams.get("window") ?? "day") as
        | "hour"
        | "day"
        | "week";
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify(makeCostFixture(window)),
      });
    });
  });

  test("renders polyline, legend total, and switches windows", async ({
    page,
  }) => {
    await page.goto("/s/alpha/meta");

    // Scope to the Meta tabpanel — the Dashboard card also has
    // aria-label="Cumulative cost" on desktop viewports.
    const region = page
      .getByRole("tabpanel", { name: /meta/i })
      .getByRole("region", { name: /cumulative cost/i });
    await expect(region).toBeVisible();

    // Polyline visible with a points attribute.
    const poly = region.locator('[data-testid="cost-polyline"]');
    await expect(poly).toBeVisible();
    const pointsAttr = await poly.getAttribute("points");
    expect(pointsAttr && pointsAttr.length).toBeTruthy();

    // Legend: 120_000 micros = $0.1200.
    await expect(region.getByText("$0.1200")).toBeVisible();
    // Token sum: input(10k) + output(5k) = 15k → "15k tokens".
    await expect(region.getByText(/15k tokens/i)).toBeVisible();

    // Window pill switch: click "Hour", verify aria-selected moves.
    await region.getByRole("tab", { name: /hour/i }).click();
    await expect(region.getByRole("tab", { name: /hour/i })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    await expect(region.getByRole("tab", { name: /day/i })).toHaveAttribute(
      "aria-selected",
      "false",
    );
  });
});
