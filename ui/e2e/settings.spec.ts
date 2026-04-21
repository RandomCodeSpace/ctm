import { test, expect } from "@playwright/test";
import { authenticate, installMocks } from "./fixtures/mocks";

/**
 * V0.2 Settings drawer — gear icon in the Dashboard top bar opens a
 * right-side drawer that seeds from GET /api/config and PATCHes edits
 * back. The daemon responds 202 then restarts itself ~1s later; we
 * don't simulate the restart here (the ConnectionBanner has its own
 * test coverage), we only assert the PATCH round-trip.
 *
 * Both handlers are mocked inline — the shared fixture file is
 * deliberately not touched until a second spec needs /api/config.
 */

const seededConfig = {
  webhook_url: "https://old.example",
  webhook_auth: "Bearer old",
  attention: {
    error_rate_pct: 20,
    error_rate_window: 30,
    idle_minutes: 5,
    quota_pct: 85,
    context_pct: 90,
    yolo_unchecked_minutes: 30,
  },
};

test.describe("SettingsDrawer", () => {
  test("opens from the gear icon, edits a threshold, and PATCHes back", async ({
    page,
  }) => {
    await authenticate(page);
    await installMocks(page);

    // Record PATCH body so the assertion can confirm the shape.
    let patchBody: unknown = null;

    await page.route("**/api/config", (route) => {
      const req = route.request();
      if (req.method() === "PATCH") {
        patchBody = req.postDataJSON();
        return route.fulfill({
          status: 202,
          contentType: "application/json",
          body: JSON.stringify({ status: "restarting" }),
        });
      }
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify(seededConfig),
      });
    });

    await page.goto("/");

    // Open the drawer from the gear button in the top bar.
    await page.getByRole("button", { name: /open settings/i }).click();

    // Form seeds from GET /api/config.
    const urlInput = page.getByLabel(/^webhook url$/i);
    await expect(urlInput).toHaveValue("https://old.example");
    const quotaInput = page.getByLabel(/^quota %$/i);
    await expect(quotaInput).toHaveValue("85");

    // Edit the idle minutes threshold.
    const idleInput = page.getByLabel(/^idle minutes$/i);
    await idleInput.fill("12");

    // Save.
    await page.getByRole("button", { name: /save & restart/i }).click();

    // Restarting banner surfaces.
    await expect(page.getByText(/daemon restarting/i)).toBeVisible();

    // PATCH body shape matches the contract.
    expect(patchBody).not.toBeNull();
    const body = patchBody as {
      webhook_url: string;
      webhook_auth: string;
      attention: { idle_minutes: number; quota_pct: number };
    };
    expect(body.webhook_url).toBe("https://old.example");
    expect(body.webhook_auth).toBe("Bearer old");
    expect(body.attention.idle_minutes).toBe(12);
    expect(body.attention.quota_pct).toBe(85);
  });

  test("locally disables submit on out-of-range value", async ({ page }) => {
    await authenticate(page);
    await installMocks(page);
    await page.route("**/api/config", (route) =>
      route.fulfill({
        contentType: "application/json",
        body: JSON.stringify(seededConfig),
      }),
    );

    await page.goto("/");
    await page.getByRole("button", { name: /open settings/i }).click();

    const quotaInput = page.getByLabel(/^quota %$/i);
    await quotaInput.fill("150");

    const submit = page.getByRole("button", { name: /save & restart/i });
    await expect(submit).toBeDisabled();
    await expect(page.getByText(/quota % must be <= 100/i)).toBeVisible();
  });

  test("close button dismisses the drawer", async ({ page }) => {
    await authenticate(page);
    await installMocks(page);
    await page.route("**/api/config", (route) =>
      route.fulfill({
        contentType: "application/json",
        body: JSON.stringify(seededConfig),
      }),
    );

    await page.goto("/");
    await page.getByRole("button", { name: /open settings/i }).click();
    await expect(page.getByLabel(/^webhook url$/i)).toBeVisible();

    // Radix Sheet auto-injects an sr-only X button also named "Close".
    // Scope to the footer's data-slot to target our explicit footer
    // button without a strict-mode collision.
    await page
      .locator('[data-slot="sheet-footer"]')
      .getByRole("button", { name: /^close$/i })
      .click();
    await expect(page.getByLabel(/^webhook url$/i)).toBeHidden();
  });
});
