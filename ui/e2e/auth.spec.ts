import { test, expect, type Route } from "@playwright/test";
import { installMocks } from "./fixtures/mocks";

test.describe("Auth (V27)", () => {
  test("unregistered → signup → dashboard", async ({ page }) => {
    await installMocks(page, { authRegistered: false, authAuthenticated: false });
    await page.goto("/");
    await expect(
      page.getByRole("heading", { name: /create your ctm account/i }),
    ).toBeVisible();

    await page.getByLabel(/username/i).fill("alice");
    await page.getByLabel(/^password$/i).fill("password123");
    await page.getByLabel(/confirm/i).fill("password123");
    await page.getByRole("button", { name: /create account/i }).click();

    await installMocks(page, { authRegistered: true, authAuthenticated: true });
    await page.reload();
    await expect(
      page.getByRole("button", { name: /new session/i }),
    ).toBeVisible();
  });

  test("registered + unauthenticated → login → dashboard", async ({ page }) => {
    await installMocks(page, { authRegistered: true, authAuthenticated: false });
    await page.goto("/");
    await expect(
      page.getByRole("heading", { name: /log in to ctm/i }),
    ).toBeVisible();

    await page.getByLabel(/username/i).fill("alice");
    await page.getByLabel(/^password$/i).fill("password123");
    await page.getByRole("button", { name: /log in/i }).click();

    await installMocks(page, { authRegistered: true, authAuthenticated: true });
    await page.reload();
    await expect(
      page.getByRole("button", { name: /new session/i }),
    ).toBeVisible();
  });

  test("login 401 surfaces error", async ({ page }) => {
    await installMocks(page, { authRegistered: true, authAuthenticated: false });
    await page.route("**/api/auth/login", (r: Route) =>
      r.fulfill({
        status: 401,
        contentType: "application/json",
        body: JSON.stringify({ error: "invalid_credentials", message: "nope" }),
      }),
    );
    await page.goto("/");
    await page.getByLabel(/username/i).fill("alice");
    await page.getByLabel(/^password$/i).fill("wrong");
    await page.getByRole("button", { name: /log in/i }).click();
    await expect(page.getByRole("alert")).toBeVisible();
  });

  test("signup 409 offers 'log in instead'", async ({ page }) => {
    await installMocks(page, { authRegistered: false, authAuthenticated: false });
    await page.route("**/api/auth/signup", (r: Route) =>
      r.fulfill({
        status: 409,
        contentType: "application/json",
        body: JSON.stringify({ error: "already_registered", message: "exists" }),
      }),
    );
    await page.goto("/");
    await page.getByLabel(/username/i).fill("alice");
    await page.getByLabel(/^password$/i).fill("password123");
    await page.getByLabel(/confirm/i).fill("password123");
    await page.getByRole("button", { name: /create account/i }).click();
    await expect(
      page.getByRole("button", { name: /log in instead/i }),
    ).toBeVisible();
    await page.getByRole("button", { name: /log in instead/i }).click();
    await expect(
      page.getByRole("heading", { name: /log in to ctm/i }),
    ).toBeVisible();
  });

  test("logout from settings returns to login", async ({ page }) => {
    await installMocks(page, { authRegistered: true, authAuthenticated: true });
    await page.goto("/");
    await page.getByRole("button", { name: /open settings/i }).click();
    await page.getByRole("button", { name: /log out/i }).click();

    await installMocks(page, { authRegistered: true, authAuthenticated: false });
    await page.reload();
    await expect(
      page.getByRole("heading", { name: /log in to ctm/i }),
    ).toBeVisible();
  });
});
