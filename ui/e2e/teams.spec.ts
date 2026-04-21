import { test, expect, type Route } from "@playwright/test";
import { authenticate, installMocks, defaultSession } from "./fixtures/mocks";

const TEAMS_PAYLOAD = {
  teams: [
    {
      id: "team-live",
      name: "Explore · 3 agents",
      dispatched_at: "2026-04-21T12:05:00Z",
      status: "running",
      members: [
        {
          subagent_id: "agent-a1",
          description: "scan repo",
          status: "running",
        },
        {
          subagent_id: "agent-a2",
          description: "read spec",
          status: "completed",
        },
        {
          subagent_id: "agent-a3",
          description: "tests",
          status: "completed",
        },
      ],
    },
    {
      id: "team-done",
      name: "Task · 2 agents",
      dispatched_at: "2026-04-21T11:50:00Z",
      status: "completed",
      summary: "Plan approved; three follow-ups logged.",
      members: [
        {
          subagent_id: "agent-b1",
          description: "planner",
          status: "completed",
        },
        {
          subagent_id: "agent-b2",
          description: "writer",
          status: "completed",
        },
      ],
    },
  ],
};

test.describe("Teams tab (V16)", () => {
  test.beforeEach(async ({ page }) => {
    await authenticate(page);
    await installMocks(page);

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

    await page.route("**/api/sessions/*/teams*", (route: Route) => {
      return route.fulfill({
        contentType: "application/json",
        body: JSON.stringify(TEAMS_PAYLOAD),
      });
    });
  });

  test("renders two teams with distinct status chips and expands to reveal members", async ({
    page,
  }) => {
    await page.goto("/s/alpha/teams");

    const region = page.getByRole("region", { name: /agent teams/i });
    await expect(region).toBeVisible();

    await expect(page.getByTestId("team-card-team-live")).toBeVisible();
    await expect(page.getByTestId("team-card-team-done")).toBeVisible();

    // Status chips colour-coded via testid on the chip element.
    await expect(page.getByTestId("team-status-running")).toBeVisible();
    await expect(page.getByTestId("team-status-completed")).toBeVisible();

    // Expand the running team and verify members appear.
    await page
      .getByTestId("team-card-team-live")
      .getByRole("button")
      .first()
      .click();
    await expect(
      page.getByTestId("team-member-agent-a1"),
    ).toBeVisible();
    await expect(
      page.getByTestId("team-member-agent-a2"),
    ).toBeVisible();
    await expect(
      page.getByTestId("team-member-agent-a3"),
    ).toBeVisible();
  });
});
