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
});
