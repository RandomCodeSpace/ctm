import { test, expect } from "@playwright/test";
import { authenticate, installMocks } from "./fixtures/mocks";

test.describe("Dashboard", () => {
  test.beforeEach(async ({ page }) => {
    await authenticate(page);
    await installMocks(page);
  });

  test("renders the seeded session card", async ({ page }) => {
    await page.goto("/");
    // Desktop auto-navigates to the top session so "alpha" appears in
    // both the list card and detail heading — assert on the link card.
    await expect(
      page.getByRole("link", { name: /alpha/i }).first(),
    ).toBeVisible();
    await expect(page.getByText(/safe/i).first()).toBeVisible();
  });

  test("shows last tool call time on the card", async ({ page }) => {
    // Seeded tool call is at 16:28; attached at 16:00. Card should carry
    // both time readings (⏵ for tool call, plain for attached).
    await page.goto("/");
    const card = page.getByRole("link", { name: /alpha/i }).first();
    await expect(card).toBeVisible();
    // ⏵ glyph rendered for the tool-call badge.
    await expect(card.locator('time[aria-label="last tool call"]')).toBeVisible();
  });

  test("renders the per-session context bar on the card", async ({ page }) => {
    // Seeded session has context_pct: 42 → bar should be visible.
    await page.goto("/");
    const card = page.getByRole("link", { name: /alpha/i }).first();
    await expect(card).toBeVisible();
    const bar = card.getByRole("progressbar", { name: /context window/i });
    await expect(bar).toBeVisible();
    await expect(bar).toHaveAttribute("aria-valuenow", "42");
  });

  test("context bar turns ember at >=90%", async ({ page }) => {
    await installMocks(page, {
      sessions: [
        {
          name: "hot",
          uuid: "00000000-0000-0000-0000-0000000000aa",
          mode: "yolo",
          workdir: "/home/dev/projects/ctm",
          created_at: "2026-04-21T10:00:00Z",
          last_attached_at: "2026-04-21T11:00:00Z",
          last_tool_call_at: new Date(Date.now() - 5_000).toISOString(),
          is_active: true,
          tmux_alive: true,
          context_pct: 95,
        },
      ],
    });
    await page.goto("/");
    const card = page.getByRole("link", { name: /hot/i }).first();
    const bar = card.getByRole("progressbar", { name: /context window/i });
    await expect(bar).toBeVisible();
    await expect(bar).toHaveAttribute("aria-valuenow", "95");
    // The coloured fill is the first child; verify ember class + width.
    const fill = bar.locator("> div").first();
    await expect(fill).toHaveClass(/bg-alert-ember/);
    await expect(fill).toHaveAttribute("style", /width:\s*95%/);
  });

  test("renders the tool-frequency sparkline when the feed has events", async ({
    page,
  }) => {
    // Seed the SSE stream with 6 tool_call events spread across 10 min.
    // The sparkline component reads from the SseProvider-populated feed
    // cache, so we route /events/all to deliver synthetic events once.
    const now = Date.now();
    const lines: string[] = [];
    for (let i = 0; i < 6; i++) {
      const ts = new Date(now - i * 60_000).toISOString();
      const id = `${now - i * 60_000}000000-0`;
      const data = JSON.stringify({
        session: "alpha",
        tool: "Bash",
        input: "",
        is_error: false,
        ts,
      });
      lines.push(`id: ${id}\nevent: tool_call\ndata: ${data}\n\n`);
    }
    await page.route(
      "**/events/all",
      (route) =>
        route.fulfill({
          status: 200,
          contentType: "text/event-stream",
          body: ": ok\n\n" + lines.join(""),
        }),
      { times: 1 },
    );
    await page.goto("/");
    const card = page.getByRole("link", { name: /alpha/i }).first();
    const svg = card.locator(
      'svg[aria-label*="tool call frequency" i]',
    );
    await expect(svg).toBeVisible();
  });

  test("renders the stale chip when tool call is older than 30 min", async ({
    page,
  }) => {
    const oldTC = new Date(Date.now() - 45 * 60_000).toISOString();
    await installMocks(page, {
      sessions: [
        {
          name: "dormant",
          uuid: "00000000-0000-0000-0000-00000000dead",
          mode: "safe",
          workdir: "/home/dev/projects/ctm",
          created_at: "2026-04-21T10:00:00Z",
          last_attached_at: "2026-04-21T11:00:00Z",
          last_tool_call_at: oldTC,
          is_active: true,
          tmux_alive: true,
        },
      ],
    });
    await page.goto("/");
    const card = page.getByRole("link", { name: /dormant/i }).first();
    await expect(card.getByLabel("stale session")).toBeVisible();
  });
});
