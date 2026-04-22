import { test, expect, type Route } from "@playwright/test";
import { authenticate, installMocks, defaultSession } from "./fixtures/mocks";

test.describe("Create session (V26)", () => {
  test.beforeEach(async ({ page }) => {
    await authenticate(page);
    await installMocks(page, {
      sessions: [
        { ...defaultSession, name: "ctm", workdir: "/home/dev/projects/ctm" },
        { ...defaultSession, name: "docsiq", workdir: "/home/dev/projects/docsiq" },
      ],
    });
  });

  test("opens modal, recents pre-fills workdir, Create navigates", async ({
    page,
  }) => {
    const postedBodies: string[] = [];
    await page.route("**/api/sessions", (route: Route) => {
      if (route.request().method() !== "POST") return route.fallback();
      postedBodies.push(route.request().postData() ?? "");
      return route.fulfill({
        status: 201,
        contentType: "application/json",
        body: JSON.stringify({
          name: "ctm",
          uuid: "u",
          mode: "yolo",
          workdir: "/home/dev/projects/ctm",
          created_at: new Date().toISOString(),
          is_active: true,
          tmux_alive: true,
        }),
      });
    });

    await page.goto("/");
    await page.getByRole("button", { name: /new session/i }).click();

    const workdir = page.getByRole("textbox", { name: /workdir/i });
    await expect(workdir).toHaveValue(/\/home\/dev\/projects\//);

    await page.getByRole("button", { name: /create/i }).click();
    await expect(page).toHaveURL(/\/s\/ctm$/);
    expect(JSON.parse(postedBodies[0])).toEqual({
      workdir: "/home/dev/projects/ctm",
    });
  });

  test("collision surfaces rename / go-to-existing", async ({ page }) => {
    await page.route("**/api/sessions", (route: Route) => {
      if (route.request().method() !== "POST") return route.fallback();
      return route.fulfill({
        status: 409,
        contentType: "application/json",
        body: JSON.stringify({
          error: "name_exists",
          message: "exists",
          session: {
            name: "ctm",
            uuid: "u",
            mode: "yolo",
            workdir: "/home/dev/projects/ctm",
          },
        }),
      });
    });

    await page.goto("/");
    await page.getByRole("button", { name: /new session/i }).click();
    await page.getByRole("button", { name: /create/i }).click();

    await expect(
      page.getByRole("button", { name: /go to existing/i }),
    ).toBeVisible();
    await page.getByRole("button", { name: /go to existing/i }).click();
    await expect(page).toHaveURL(/\/s\/ctm$/);
  });

  test("fills initial prompt textarea and posts initial_prompt", async ({
    page,
  }) => {
    const postedBodies: string[] = [];
    await page.route("**/api/sessions", (route: Route) => {
      if (route.request().method() !== "POST") return route.fallback();
      postedBodies.push(route.request().postData() ?? "");
      return route.fulfill({
        status: 201,
        contentType: "application/json",
        body: JSON.stringify({
          name: "ctm",
          uuid: "u",
          mode: "yolo",
          workdir: "/home/dev/projects/ctm",
          created_at: new Date().toISOString(),
          is_active: true,
          tmux_alive: true,
        }),
      });
    });

    await page.goto("/");
    await page.getByRole("button", { name: /new session/i }).click();
    await page
      .getByRole("textbox", { name: /initial prompt/i })
      .fill("review the diff");
    await page.getByRole("button", { name: /create/i }).click();

    await expect(page).toHaveURL(/\/s\/ctm$/);
    expect(JSON.parse(postedBodies[0])).toEqual({
      workdir: "/home/dev/projects/ctm",
      initial_prompt: "review the diff",
    });
  });
});
