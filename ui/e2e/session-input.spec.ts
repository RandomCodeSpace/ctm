import { test, expect, type Route } from "@playwright/test";
import { authenticate, installMocks, defaultSession } from "./fixtures/mocks";

/**
 * V25 Session Input — e2e coverage:
 *  - Bar is visible on a yolo-mode session and hidden on safe
 *  - Approve button POSTs { preset: "yes" } to the correct URL
 *  - Server 409 tmux_dead surfaces inline via role=status
 */
test.describe("SessionInputBar", () => {
  const yolo = { ...defaultSession, name: "alpha", mode: "yolo" as const };
  const safe = { ...defaultSession, name: "safe", mode: "safe" as const };

  test.beforeEach(async ({ page }) => {
    await authenticate(page);
    await installMocks(page, { sessions: [yolo, safe] });
  });

  test("shows the bar on yolo sessions and hides it on safe", async ({
    page,
  }) => {
    await page.goto("/s/alpha");
    await expect(
      page.getByRole("button", { name: /approve/i }),
    ).toBeVisible();

    await page.goto("/s/safe");
    await expect(
      page.getByRole("button", { name: /approve/i }),
    ).toHaveCount(0);
  });

  test("Approve POSTs preset=yes to the right URL", async ({ page }) => {
    const posted: Array<{ url: string; body: string }> = [];
    await page.route("**/api/sessions/alpha/input", (route: Route) => {
      posted.push({
        url: route.request().url(),
        body: route.request().postData() ?? "",
      });
      return route.fulfill({ status: 204 });
    });

    await page.goto("/s/alpha");
    await page.getByRole("button", { name: /approve/i }).click();

    await expect.poll(() => posted.length).toBeGreaterThan(0);
    expect(posted[0].url).toMatch(/\/api\/sessions\/alpha\/input$/);
    expect(JSON.parse(posted[0].body)).toEqual({ preset: "yes" });
  });

  test("surfaces tmux_dead error inline", async ({ page }) => {
    await page.route("**/api/sessions/alpha/input", (route: Route) =>
      route.fulfill({
        status: 409,
        contentType: "application/json",
        body: JSON.stringify({
          error: "tmux_dead",
          message: "session tmux has exited",
        }),
      }),
    );

    await page.goto("/s/alpha");
    await page.getByRole("button", { name: /approve/i }).click();

    await expect(
      page.getByRole("status").filter({ hasText: /tmux|could not/i }),
    ).toBeVisible();
  });
});
