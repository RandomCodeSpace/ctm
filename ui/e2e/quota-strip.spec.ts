import { test, expect } from "@playwright/test";
import { authenticate, installMocks } from "./fixtures/mocks";

test.describe("QuotaStrip", () => {
  test.beforeEach(async ({ page }) => {
    await authenticate(page);
    await installMocks(page);
  });

  test("shows future reset time, not 0 sec (regression 4e9694f)", async ({
    page,
  }) => {
    // Default mock has 5h reset 45 minutes out, weekly 3 hours out.
    // Before the relativeFuture fix, both rendered "0 sec".
    await page.goto("/");

    const strip = page.getByRole("region", { name: /rate limit usage/i });
    await expect(strip).toBeVisible();

    const text = await strip.innerText();
    expect(text).not.toContain("resets in 0 sec");
    expect(text).toMatch(/resets in \d+ (min|hr|day)/);
  });

  test("renders percentage values", async ({ page }) => {
    await page.goto("/");
    const strip = page.getByRole("region", { name: /rate limit usage/i });
    await expect(strip).toContainText("24%"); // 5h
    await expect(strip).toContainText("48%"); // weekly
  });
});
