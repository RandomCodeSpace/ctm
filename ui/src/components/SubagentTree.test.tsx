import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { SubagentTree } from "@/components/SubagentTree";
import type { SubagentNode } from "@/hooks/useSubagents";

/**
 * V15 — SubagentTree renderer tests. We drive the useSubagents hook
 * by intercepting fetch on the `/api/sessions/{name}/subagents`
 * endpoint — matches the same pattern used by LogDiskUsage.test.tsx.
 */

function renderWithQuery(ui: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

function mockSubagents(nodes: SubagentNode[]) {
  return vi.fn(async () =>
    new Response(JSON.stringify({ subagents: nodes }), {
      status: 200,
      headers: { "content-type": "application/json" },
    }),
  );
}

const fiveNodes: SubagentNode[] = [
  {
    id: "agent-5",
    parent_id: null,
    type: "Explore",
    description: "look for README",
    started_at: "2026-04-21T12:05:00Z",
    tool_calls: 4,
    status: "running",
  },
  {
    id: "agent-4",
    parent_id: null,
    type: "Task",
    description: "refactor auth",
    started_at: "2026-04-21T12:04:00Z",
    stopped_at: "2026-04-21T12:04:30Z",
    tool_calls: 2,
    status: "failed",
  },
  {
    id: "agent-3",
    parent_id: null,
    type: "Explore",
    description: "read spec",
    started_at: "2026-04-21T12:03:00Z",
    stopped_at: "2026-04-21T12:03:45Z",
    tool_calls: 6,
    status: "completed",
  },
  {
    id: "agent-2",
    parent_id: null,
    type: "Explore",
    description: "scan tests",
    started_at: "2026-04-21T12:02:00Z",
    stopped_at: "2026-04-21T12:02:40Z",
    tool_calls: 3,
    status: "completed",
  },
  {
    id: "agent-1",
    parent_id: null,
    type: "Task",
    description: "init branch",
    started_at: "2026-04-21T12:01:00Z",
    stopped_at: "2026-04-21T12:01:20Z",
    tool_calls: 1,
    status: "completed",
  },
];

describe("SubagentTree", () => {
  let originalFetch: typeof globalThis.fetch;
  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });
  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("renders all five rows from the fixture and tags status dots correctly", async () => {
    globalThis.fetch = mockSubagents(fiveNodes);
    renderWithQuery(<SubagentTree sessionName="alpha" />);

    await waitFor(() =>
      expect(
        screen.getByTestId("subagent-row-agent-5"),
      ).toBeInTheDocument(),
    );

    expect(screen.getByTestId("subagent-row-agent-1")).toBeInTheDocument();
    expect(screen.getByTestId("subagent-row-agent-2")).toBeInTheDocument();
    expect(screen.getByTestId("subagent-row-agent-3")).toBeInTheDocument();
    expect(screen.getByTestId("subagent-row-agent-4")).toBeInTheDocument();
    // Running, one failed, three completed — three dots for completed.
    expect(screen.getAllByTestId("status-dot-running")).toHaveLength(1);
    expect(screen.getAllByTestId("status-dot-failed")).toHaveLength(1);
    expect(screen.getAllByTestId("status-dot-completed")).toHaveLength(3);
  });

  it("expands a row to reveal its tool-call counter", async () => {
    globalThis.fetch = mockSubagents(fiveNodes);
    const user = userEvent.setup();
    renderWithQuery(<SubagentTree sessionName="alpha" />);

    const row = await screen.findByTestId("subagent-row-agent-3");
    expect(screen.queryByTestId("subagent-detail-agent-3")).toBeNull();
    await user.click(row);
    await waitFor(() =>
      expect(
        screen.getByTestId("subagent-detail-agent-3"),
      ).toBeInTheDocument(),
    );
    // The tool_calls count (6) should render inside the expanded body.
    const detail = screen.getByTestId("subagent-detail-agent-3");
    expect(detail).toHaveTextContent("6");
  });

  it("renders the empty state when the server returns no subagents", async () => {
    globalThis.fetch = mockSubagents([]);
    renderWithQuery(<SubagentTree sessionName="alpha" />);
    await waitFor(() =>
      expect(
        screen.getByText(/no subagents for this session/i),
      ).toBeInTheDocument(),
    );
  });
});
