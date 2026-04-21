import { test, expect } from "@playwright/test";
import { authenticate, installMocks } from "./fixtures/mocks";

/**
 * V20 doctor panel. Mocks /api/doctor at page level (the shared
 * fixtures don't include it yet; adding it inline here keeps the
 * fixtures file untouched for now — promote if a second spec needs it).
 */
test.describe("DoctorPanel", () => {
  test.beforeEach(async ({ page }) => {
    await authenticate(page);
    await installMocks(page);
    await page.route("**/api/doctor", (route) =>
      route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({
          checks: [
            { name: "dep:tmux", status: "ok", message: "/usr/bin/tmux" },
            {
              name: "env:PATH",
              status: "warn",
              message: "short",
              remediation: "export PATH=...",
            },
            {
              name: "serve:token",
              status: "err",
              message: "missing",
              remediation: "run ctm doctor",
            },
          ],
        }),
      }),
    );
  });

  test("navigates to /doctor from the dashboard top bar and renders rows", async ({
    page,
  }) => {
    await page.goto("/");

    // Dashboard → Doctor via the Stethoscope link in the top bar.
    await page
      .getByRole("link", { name: /open doctor diagnostics/i })
      .click();
    await expect(page).toHaveURL(/\/doctor$/);

    // One row per check.
    await expect(page.getByText("dep:tmux")).toBeVisible();
    await expect(page.getByText("env:PATH")).toBeVisible();
    await expect(page.getByText("serve:token")).toBeVisible();

    // Colour-coded dots present with the expected class hooks.
    const okDot = page.getByLabel("check ok");
    const warnDot = page.getByLabel("check warn");
    const errDot = page.getByLabel("check err");
    await expect(okDot).toBeVisible();
    await expect(warnDot).toBeVisible();
    await expect(errDot).toBeVisible();

    // Classnames carry the status tokens.
    await expect(okDot).toHaveClass(/bg-live-dot/);
    await expect(warnDot).toHaveClass(/bg-accent-gold/);
    await expect(errDot).toHaveClass(/bg-alert-ember/);
  });

  test("expanding a row reveals its remediation", async ({ page }) => {
    await page.goto("/doctor");

    await expect(page.getByText("env:PATH")).toBeVisible();
    // Remediation hidden by default.
    await expect(
      page.getByText("export PATH=...", { exact: false }),
    ).toHaveCount(0);

    // Click the env:PATH row to expand.
    await page.getByRole("button", { name: /env:PATH.*warn/i }).click();
    await expect(
      page.getByText("export PATH=...", { exact: false }),
    ).toBeVisible();
  });

  test("re-run button triggers a refetch", async ({ page }) => {
    let hits = 0;
    await page.unroute("**/api/doctor");
    await page.route("**/api/doctor", (route) => {
      hits += 1;
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify({
          checks: [
            { name: "dep:tmux", status: "ok", message: "/usr/bin/tmux" },
          ],
        }),
      });
    });

    await page.goto("/doctor");
    await expect(page.getByText("dep:tmux")).toBeVisible();
    const initial = hits;

    await page.getByRole("button", { name: /re-run checks/i }).click();
    await expect.poll(() => hits).toBeGreaterThan(initial);
  });
});
