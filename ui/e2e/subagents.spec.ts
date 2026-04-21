import { test, expect, type Route } from "@playwright/test";
import { authenticate, installMocks, defaultSession } from "./fixtures/mocks";

const SUBAGENTS_PAYLOAD = {
  subagents: [
    {
      id: "agent-3",
      parent_id: null,
      type: "Explore",
      description: "scan the repo",
      started_at: "2026-04-21T12:05:00Z",
      tool_calls: 3,
      status: "running",
    },
    {
      id: "agent-2",
      parent_id: null,
      type: "Task",
      description: "refactor auth",
      started_at: "2026-04-21T12:04:00Z",
      stopped_at: "2026-04-21T12:04:30Z",
      tool_calls: 2,
      status: "completed",
    },
    {
      id: "agent-1",
      parent_id: null,
      type: "Explore",
      description: "read spec",
      started_at: "2026-04-21T12:03:00Z",
      stopped_at: "2026-04-21T12:03:20Z",
      tool_calls: 5,
      status: "failed",
    },
  ],
};

test.describe("Subagents tab (V15)", () => {
  test.beforeEach(async ({ page }) => {
    await authenticate(page);
    await installMocks(page);

    // Per-session GET — dashboard covers the list; SessionDetail hits
    // /api/sessions/{name} on mount, so we stub it to the default row.
    await page.route("**/api/sessions/**", (route: Route) => {
      const url = new URL(route.request().url());
      if (
        url.pathname.endsWith("/feed") ||
        url.pathname === "/api/sessions" ||
        url.pathname.endsWith("/subagents") ||
        url.pathname.endsWith("/teams") ||
        url.pathname.endsWith("/checkpoints") ||
        url.pathname.endsWith("/feed/history")
      ) {
        return route.fallback();
      }
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify(defaultSession),
      });
    });

    await page.route(
      "**/api/sessions/*/subagents*",
      (route: Route) => {
        return route.fulfill({
          contentType: "application/json",
          body: JSON.stringify(SUBAGENTS_PAYLOAD),
        });
      },
    );
  });

  test("renders all three subagent rows and expands one", async ({ page }) => {
    await page.goto("/s/alpha/subagents");

    const region = page.getByRole("region", { name: /subagent tree/i });
    await expect(region).toBeVisible();

    // All three subagent rows visible.
    await expect(
      page.getByTestId("subagent-row-agent-1"),
    ).toBeVisible();
    await expect(
      page.getByTestId("subagent-row-agent-2"),
    ).toBeVisible();
    await expect(
      page.getByTestId("subagent-row-agent-3"),
    ).toBeVisible();

    // Descriptions surface in the rows.
    await expect(region).toContainText("scan the repo");
    await expect(region).toContainText("refactor auth");
    await expect(region).toContainText("read spec");

    // Click to expand agent-3 reveals its tool-call counter.
    await page.getByTestId("subagent-row-agent-3").click();
    const detail = page.getByTestId("subagent-detail-agent-3");
    await expect(detail).toBeVisible();
    await expect(detail).toContainText("3"); // tool_calls
  });
});
