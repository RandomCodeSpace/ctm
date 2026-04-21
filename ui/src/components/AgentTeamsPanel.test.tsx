import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AgentTeamsPanel } from "@/components/AgentTeamsPanel";
import type { Team } from "@/hooks/useTeams";

function renderWithQuery(ui: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

function mockTeams(teams: Team[]) {
  return vi.fn(async () =>
    new Response(JSON.stringify({ teams }), {
      status: 200,
      headers: { "content-type": "application/json" },
    }),
  );
}

const twoTeams: Team[] = [
  {
    id: "team-fresh",
    name: "Explore · 3 agents",
    dispatched_at: "2026-04-21T12:05:00Z",
    status: "running",
    members: [
      { subagent_id: "agent-a1", description: "scan repo", status: "running" },
      { subagent_id: "agent-a2", description: "read spec", status: "completed" },
      { subagent_id: "agent-a3", description: "test", status: "completed" },
    ],
  },
  {
    id: "team-old",
    name: "Task · 2 agents",
    dispatched_at: "2026-04-21T11:50:00Z",
    status: "completed",
    summary: "Plan approved; 3 follow-ups logged.",
    members: [
      { subagent_id: "agent-b1", description: "planner", status: "completed" },
      { subagent_id: "agent-b2", description: "writer", status: "completed" },
    ],
  },
];

describe("AgentTeamsPanel", () => {
  let originalFetch: typeof globalThis.fetch;
  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("renders a card per team with a status chip", async () => {
    globalThis.fetch = mockTeams(twoTeams);
    renderWithQuery(<AgentTeamsPanel sessionName="alpha" />);

    await waitFor(() =>
      expect(screen.getByTestId("team-card-team-fresh")).toBeInTheDocument(),
    );
    expect(screen.getByTestId("team-card-team-old")).toBeInTheDocument();

    // Status chips carry data-testids for running/completed.
    expect(screen.getByTestId("team-status-running")).toBeInTheDocument();
    expect(screen.getByTestId("team-status-completed")).toBeInTheDocument();
  });

  it("expanding a team reveals its members", async () => {
    globalThis.fetch = mockTeams(twoTeams);
    const user = userEvent.setup();
    renderWithQuery(<AgentTeamsPanel sessionName="alpha" />);

    const freshCard = await screen.findByTestId("team-card-team-fresh");
    // Before expand: member rows not yet in DOM.
    expect(screen.queryByTestId("team-member-agent-a1")).toBeNull();
    await user.click(freshCard.querySelector("button") as HTMLElement);
    await waitFor(() =>
      expect(screen.getByTestId("team-member-agent-a1")).toBeInTheDocument(),
    );
    expect(screen.getByTestId("team-member-agent-a2")).toBeInTheDocument();
    expect(screen.getByTestId("team-member-agent-a3")).toBeInTheDocument();
  });

  it("teams with no summary hide the blockquote", async () => {
    globalThis.fetch = mockTeams(twoTeams);
    const user = userEvent.setup();
    renderWithQuery(<AgentTeamsPanel sessionName="alpha" />);

    const fresh = await screen.findByTestId("team-card-team-fresh");
    await user.click(fresh.querySelector("button") as HTMLElement);
    await waitFor(() =>
      expect(screen.getByTestId("team-member-agent-a1")).toBeInTheDocument(),
    );
    // No blockquote for team without summary.
    const detail = screen.getByTestId("team-detail-team-fresh");
    expect(detail.querySelector("blockquote")).toBeNull();

    // The second team (team-old) has a summary — expanding shows it.
    const old = screen.getByTestId("team-card-team-old");
    await user.click(old.querySelector("button") as HTMLElement);
    await waitFor(() =>
      expect(
        screen.getByTestId("team-detail-team-old").querySelector("blockquote"),
      ).not.toBeNull(),
    );
    expect(
      screen.getByTestId("team-detail-team-old").querySelector("blockquote")!
        .textContent,
    ).toMatch(/follow-ups logged/);
  });

  it("renders empty state when no teams exist", async () => {
    globalThis.fetch = mockTeams([]);
    renderWithQuery(<AgentTeamsPanel sessionName="alpha" />);
    await waitFor(() =>
      expect(
        screen.getByText(/no teams for this session/i),
      ).toBeInTheDocument(),
    );
  });
});
