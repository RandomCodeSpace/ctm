import { test, expect } from "@playwright/test";
import { installMocks } from "./fixtures/mocks";

test.describe("Auth / token paste flow", () => {
  test("shows paste screen when no token is stored", async ({ page }) => {
    await installMocks(page, { authenticated: false });
    await page.goto("/");
    // The paste screen headline is just "ctm"; assert on the paste
    // textarea + its bearer-token label to avoid coupling to copy.
    await expect(page.getByRole("textbox")).toBeVisible();
    await expect(page.getByText(/bearer token/i)).toBeVisible();
  });

  test("accepting a token transitions to the dashboard", async ({ page }) => {
    await installMocks(page, { authenticated: false });
    await page.goto("/");

    const input = page.getByRole("textbox");
    await input.fill("pasted-token-value");

    // Swap bootstrap mock to 200 so form submission passes auth.
    await installMocks(page, { authenticated: true });

    await page.getByRole("button", { name: /continue/i }).click();
    await expect(
      page.getByRole("link", { name: /alpha/i }).first(),
    ).toBeVisible();
  });
});
