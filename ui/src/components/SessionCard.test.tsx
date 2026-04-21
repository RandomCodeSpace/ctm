import { describe, expect, it } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { MemoryRouter } from "react-router";
import { SessionCard } from "@/components/SessionCard";
import type { Session, Attention } from "@/hooks/useSessions";

function makeSession(overrides: Partial<Session> = {}): Session {
  return {
    name: "claude",
    uuid: "00000000-0000-0000-0000-000000000001",
    mode: "yolo",
    workdir: "/home/dev/projects/ctm",
    created_at: new Date(Date.now() - 60_000).toISOString(),
    last_attached_at: new Date(Date.now() - 30_000).toISOString(),
    is_active: true,
    tmux_alive: true,
    last_tool_call_at: new Date(Date.now() - 5_000).toISOString(),
    context_pct: 42,
    ...overrides,
  };
}

function renderCard(session: Session) {
  return render(
    <MemoryRouter>
      <SessionCard session={session} />
    </MemoryRouter>,
  );
}

const ATTENTION_STATES: Attention["state"][] = [
  "clear",
  "error_burst",
  "stalled",
  "quota_low",
  "permission_request",
  "context_high",
  "long_session",
  "tmux_dead",
];

describe("SessionCard", () => {
  it("renders the canonical metadata for the clear state", () => {
    renderCard(makeSession({ attention: { state: "clear" } }));
    expect(screen.getByText("claude")).toBeInTheDocument();
    expect(screen.getByText("yolo")).toBeInTheDocument();
    expect(screen.getByTitle("/home/dev/projects/ctm")).toBeInTheDocument();
    // 42 → "42%"
    expect(screen.getByText("42%")).toBeInTheDocument();
  });

  it("renders all 7 attention states correctly", () => {
    for (const state of ATTENTION_STATES) {
      const { unmount, container } = renderCard(
        makeSession({
          name: `s-${state}`,
          attention: { state, details: state === "clear" ? undefined : "detail" },
        }),
      );

      const link = container.querySelector("a");
      expect(link).not.toBeNull();
      const attentive = link!.dataset.attentive === "true";

      // Presence of an AttentionLabel == status role whose aria-label starts
      // with anything other than "session" (HealthDot has aria-label="session <state>").
      const attentionLabels = within(link!)
        .queryAllByRole("status")
        .filter((el) => {
          const label = el.getAttribute("aria-label") ?? "";
          return !label.startsWith("session ");
        });

      if (state === "clear") {
        expect(attentive).toBe(false);
        expect(attentionLabels).toHaveLength(0);
      } else {
        expect(attentive).toBe(true);
        // The ember-red border + at least one AttentionLabel.
        expect(attentionLabels.length).toBeGreaterThan(0);
      }

      unmount();
    }
  });

  it("links to the session detail route", () => {
    renderCard(makeSession({ name: "weird name" }));
    const a = screen.getByRole("link");
    expect(a.getAttribute("href")).toBe("/s/weird%20name");
  });

  it("renders the active selection marker via aria-current", () => {
    render(
      <MemoryRouter>
        <SessionCard session={makeSession()} active />
      </MemoryRouter>,
    );
    expect(screen.getByRole("link")).toHaveAttribute("aria-current", "page");
  });

  it("shows the stale chip when tool call is older than the threshold", () => {
    // 45 min old tool call with live tmux → stale.
    renderCard(
      makeSession({
        last_tool_call_at: new Date(Date.now() - 45 * 60_000).toISOString(),
      }),
    );
    expect(screen.getByLabelText("stale session")).toBeInTheDocument();
  });

  it("hides the stale chip when tmux is dead (attention takes over)", () => {
    renderCard(
      makeSession({
        tmux_alive: false,
        last_tool_call_at: new Date(Date.now() - 45 * 60_000).toISOString(),
      }),
    );
    expect(screen.queryByLabelText("stale session")).not.toBeInTheDocument();
  });

  it("hides the stale chip when tool call is recent", () => {
    renderCard(makeSession()); // default has a 5 s old tool call
    expect(screen.queryByLabelText("stale session")).not.toBeInTheDocument();
  });

  it("renders the per-session context bar at gold for 80%", () => {
    renderCard(makeSession({ context_pct: 80 }));
    const bar = screen.getByRole("progressbar", { name: /context/i });
    expect(bar).toBeInTheDocument();
    expect(bar).toHaveAttribute("aria-valuenow", "80");
    // Inner fill carries the colour class.
    const fill = bar.firstElementChild as HTMLElement | null;
    expect(fill).not.toBeNull();
    expect(fill!.className).toContain("bg-accent-gold");
    expect(fill!.style.width).toBe("80%");
  });

  it("renders the per-session context bar at ember for 95%", () => {
    renderCard(makeSession({ context_pct: 95 }));
    const bar = screen.getByRole("progressbar", { name: /context/i });
    expect(bar).toHaveAttribute("aria-valuenow", "95");
    const fill = bar.firstElementChild as HTMLElement | null;
    expect(fill!.className).toContain("bg-alert-ember");
  });

  it("hides the per-session context bar when context_pct is undefined", () => {
    renderCard(makeSession({ context_pct: undefined }));
    expect(
      screen.queryByRole("progressbar", { name: /context/i }),
    ).not.toBeInTheDocument();
  });
});
