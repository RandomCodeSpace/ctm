import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter, Route, Routes } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { SessionDetail } from "@/routes/SessionDetail";
import type { Session } from "@/hooks/useSessions";
import type { CheckpointsResponse } from "@/hooks/useCheckpoints";
import { TOKEN_KEY } from "@/lib/api";

/*
 * Tests focus on SessionDetail's own contract: route -> tab mapping,
 * loading/empty/error states for the Checkpoints + Meta tabs that
 * SessionDetail actually owns, and tab switching via the design-system
 * Tabs (role="tab"). The Pane / Feed / Subagents / Teams sub-trees and
 * the live SessionInputBar pull from SSE / mutation hooks that have
 * their own dedicated test files (and require fetch-event-source
 * machinery jsdom can't run); we mock those modules inline so this
 * suite stays a unit test of SessionDetail itself.
 */

vi.mock("@/components/PaneView", () => ({
  PaneView: ({ sessionName }: { sessionName: string }) => (
    <div data-testid="pane-stub">{`pane:${sessionName}`}</div>
  ),
}));

vi.mock("@/components/FeedStream", () => ({
  FeedStream: ({
    sessionName,
    bashOnly,
  }: {
    sessionName: string;
    bashOnly?: boolean;
  }) => (
    <div data-testid="feed-stub" data-bash={bashOnly ? "1" : "0"}>
      {`feed:${sessionName}`}
    </div>
  ),
}));

vi.mock("@/components/SubagentTree", () => ({
  SubagentTree: ({ sessionName }: { sessionName: string }) => (
    <div data-testid="subagents-stub">{`subagents:${sessionName}`}</div>
  ),
}));

vi.mock("@/components/AgentTeamsPanel", () => ({
  AgentTeamsPanel: ({ sessionName }: { sessionName: string }) => (
    <div data-testid="teams-stub">{`teams:${sessionName}`}</div>
  ),
}));

vi.mock("@/components/SessionInputBar", () => ({
  SessionInputBar: ({
    sessionName,
    mode,
  }: {
    sessionName: string;
    mode: "yolo" | "safe";
  }) => (
    <div data-testid="input-bar-stub">{`bar:${sessionName}:${mode}`}</div>
  ),
}));

vi.mock("@/components/LogDiskUsage", () => ({
  LogDiskUsage: () => <div data-testid="logs-stub" />,
}));

vi.mock("@/components/CostChart", () => ({
  CostChart: ({ sessionName }: { sessionName?: string }) => (
    <div data-testid="cost-stub">{`cost:${sessionName ?? ""}`}</div>
  ),
}));

vi.mock("@/components/RevertSheet", () => ({
  RevertSheet: ({ checkpoint }: { checkpoint: { sha: string } | null }) => (
    <div data-testid="revert-sheet" data-open={checkpoint ? "1" : "0"}>
      {checkpoint ? `revert:${checkpoint.sha}` : ""}
    </div>
  ),
}));

vi.mock("@/components/DiffSheet", () => ({
  DiffSheet: ({ checkpoint }: { checkpoint: { sha: string } | null }) => (
    <div data-testid="diff-sheet" data-open={checkpoint ? "1" : "0"}>
      {checkpoint ? `diff:${checkpoint.sha}` : ""}
    </div>
  ),
}));

const SESSION_NAME = "alpha";

const baseSession: Session = {
  name: SESSION_NAME,
  uuid: "11111111-2222-3333-4444-555555555555",
  mode: "yolo",
  workdir: "/home/dev/projects/ctm",
  created_at: "2026-04-01T10:00:00Z",
  last_attached_at: "2026-04-21T12:00:00Z",
  last_tool_call_at: "2026-04-21T12:05:00Z",
  is_active: true,
  tmux_alive: true,
  context_pct: 42,
  tokens: { input_tokens: 1200, output_tokens: 340, cache_tokens: 9100 },
  attention: { state: "clear" },
};

interface RouteFetchState {
  sessionResponse?: () => Response;
  checkpointsResponse?: () => Response;
}

function buildFetchStub(state: RouteFetchState): typeof globalThis.fetch {
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();

    if (url.includes(`/api/sessions/${SESSION_NAME}/checkpoints`)) {
      return state.checkpointsResponse
        ? state.checkpointsResponse()
        : new Response(
            JSON.stringify({ git_workdir: true, checkpoints: [] }),
            { status: 200, headers: { "content-type": "application/json" } },
          );
    }

    if (url.includes(`/api/sessions/${SESSION_NAME}`)) {
      return state.sessionResponse
        ? state.sessionResponse()
        : new Response(JSON.stringify(baseSession), {
            status: 200,
            headers: { "content-type": "application/json" },
          });
    }

    return new Response("not found", { status: 404 });
  }) as unknown as typeof globalThis.fetch;
}

function renderAt(path: string) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/s/:name" element={<SessionDetail />} />
          <Route path="/s/:name/feed" element={<SessionDetail />} />
          <Route
            path="/s/:name/checkpoints"
            element={<SessionDetail />}
          />
          <Route path="/s/:name/subagents" element={<SessionDetail />} />
          <Route path="/s/:name/teams" element={<SessionDetail />} />
          <Route path="/s/:name/meta" element={<SessionDetail />} />
          <Route path="/" element={<div>dashboard</div>} />
        </Routes>
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("SessionDetail", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    localStorage.setItem(TOKEN_KEY, "test-token");
    sessionStorage.clear();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    localStorage.clear();
    sessionStorage.clear();
    vi.restoreAllMocks();
  });

  it("renders the Pane tab by default at /s/:name", async () => {
    globalThis.fetch = buildFetchStub({});
    renderAt(`/s/${SESSION_NAME}`);

    // session-name title in the page header
    expect(
      await screen.findByRole("heading", { name: SESSION_NAME }),
    ).toBeInTheDocument();

    // Pane stub renders only when the pane tab is active
    expect(await screen.findByTestId("pane-stub")).toHaveTextContent(
      `pane:${SESSION_NAME}`,
    );

    // Tabs are exposed as role=tab buttons
    const paneTab = screen.getByRole("tab", { name: "Pane" });
    expect(paneTab).toHaveAttribute("aria-selected", "true");
  });

  it("renders the input bar with the session's mode", async () => {
    globalThis.fetch = buildFetchStub({});
    renderAt(`/s/${SESSION_NAME}`);

    await waitFor(() => {
      expect(screen.getByTestId("input-bar-stub")).toHaveTextContent(
        `bar:${SESSION_NAME}:yolo`,
      );
    });
  });

  it("does not render the input bar before the session loads", () => {
    // Session fetch never resolves in this case — keep it pending.
    globalThis.fetch = vi.fn(
      () => new Promise<Response>(() => {}),
    ) as unknown as typeof globalThis.fetch;

    renderAt(`/s/${SESSION_NAME}`);
    expect(screen.queryByTestId("input-bar-stub")).not.toBeInTheDocument();
    // Header title is the session name regardless of session payload
    expect(
      screen.getByRole("heading", { name: SESSION_NAME }),
    ).toBeInTheDocument();
  });

  it("opens the Feed tab when navigated to /s/:name/feed", async () => {
    globalThis.fetch = buildFetchStub({});
    renderAt(`/s/${SESSION_NAME}/feed`);

    expect(await screen.findByTestId("feed-stub")).toHaveTextContent(
      `feed:${SESSION_NAME}`,
    );
    expect(screen.getByRole("tab", { name: "Feed" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
    // Feed filter chip strip rendered (All + Bash)
    expect(
      screen.getByRole("tablist", { name: /feed filter/i }),
    ).toBeInTheDocument();
    expect(screen.getByRole("tab", { name: "All" })).toHaveAttribute(
      "aria-selected",
      "true",
    );
  });

  it("toggles Feed filter to bash and persists across remounts via sessionStorage", async () => {
    globalThis.fetch = buildFetchStub({});
    const user = userEvent.setup();
    const { unmount } = renderAt(`/s/${SESSION_NAME}/feed`);

    await screen.findByTestId("feed-stub");

    expect(screen.getByTestId("feed-stub")).toHaveAttribute("data-bash", "0");
    await user.click(screen.getByRole("tab", { name: "Bash" }));
    expect(screen.getByTestId("feed-stub")).toHaveAttribute("data-bash", "1");

    // Persisted to sessionStorage
    expect(
      sessionStorage.getItem(`ctm.feed.filter.${SESSION_NAME}`),
    ).toBe("bash");

    unmount();

    // Remount — selection survives
    renderAt(`/s/${SESSION_NAME}/feed`);
    expect(await screen.findByTestId("feed-stub")).toHaveAttribute(
      "data-bash",
      "1",
    );
  });

  it("renders Subagents and Teams stubs on their respective routes", async () => {
    globalThis.fetch = buildFetchStub({});

    const sub = renderAt(`/s/${SESSION_NAME}/subagents`);
    expect(await screen.findByTestId("subagents-stub")).toHaveTextContent(
      `subagents:${SESSION_NAME}`,
    );
    sub.unmount();

    renderAt(`/s/${SESSION_NAME}/teams`);
    expect(await screen.findByTestId("teams-stub")).toHaveTextContent(
      `teams:${SESSION_NAME}`,
    );
  });

  it("Meta tab renders a definition list with name / uuid / mode / workdir", async () => {
    globalThis.fetch = buildFetchStub({});
    renderAt(`/s/${SESSION_NAME}/meta`);

    // Wait for session payload to resolve so MetaList renders
    await screen.findByText("uuid");
    expect(screen.getByText("name")).toBeInTheDocument();
    expect(screen.getByText("mode")).toBeInTheDocument();
    expect(screen.getByText("workdir")).toBeInTheDocument();
    // UUID printed in a <code> element
    expect(screen.getByText(baseSession.uuid)).toBeInTheDocument();
    // The mode appears in MetaList in uppercase. The header badge also
    // renders "yolo"; either way it's present, so just assert presence.
    expect(screen.getAllByText(/yolo/i).length).toBeGreaterThan(0);

    // Sibling components mounted under Meta
    expect(screen.getByTestId("logs-stub")).toBeInTheDocument();
    expect(screen.getByTestId("cost-stub")).toHaveTextContent(
      `cost:${SESSION_NAME}`,
    );
  });

  it("Meta tab handles a session without optional last_* / tokens / context_pct", async () => {
    const minimal: Session = {
      ...baseSession,
      last_attached_at: undefined,
      last_tool_call_at: undefined,
      tokens: undefined,
      context_pct: undefined,
      attention: undefined,
    };
    globalThis.fetch = buildFetchStub({
      sessionResponse: () =>
        new Response(JSON.stringify(minimal), {
          status: 200,
          headers: { "content-type": "application/json" },
        }),
    });
    renderAt(`/s/${SESSION_NAME}/meta`);

    await screen.findByText("uuid");
    // last attached -> "never"
    expect(screen.getByText("never")).toBeInTheDocument();
    // last tool call -> "none"
    expect(screen.getByText("none")).toBeInTheDocument();
    // attention -> "clear"
    expect(screen.getByText("clear")).toBeInTheDocument();
  });

  it("Checkpoints tab — empty state when git workdir but zero checkpoints", async () => {
    globalThis.fetch = buildFetchStub({});
    renderAt(`/s/${SESSION_NAME}/checkpoints`);

    expect(
      await screen.findByText(/no checkpoints/i),
    ).toBeInTheDocument();
  });

  it("Checkpoints tab — non-git workdir banner", async () => {
    const empty: CheckpointsResponse = {
      git_workdir: false,
      checkpoints: [],
    };
    globalThis.fetch = buildFetchStub({
      checkpointsResponse: () =>
        new Response(JSON.stringify(empty), {
          status: 200,
          headers: { "content-type": "application/json" },
        }),
    });
    renderAt(`/s/${SESSION_NAME}/checkpoints`);

    expect(
      await screen.findByText(/checkpoints need a git repo/i),
    ).toBeInTheDocument();
  });

  it("Checkpoints tab — error state on 500", async () => {
    globalThis.fetch = buildFetchStub({
      checkpointsResponse: () =>
        new Response(JSON.stringify({ error: "boom" }), {
          status: 500,
          headers: { "content-type": "application/json" },
        }),
    });
    renderAt(`/s/${SESSION_NAME}/checkpoints`);

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/could not load checkpoints/i);
  });

  it("Checkpoints tab — populated list renders one row per checkpoint", async () => {
    const populated: CheckpointsResponse = {
      git_workdir: true,
      checkpoints: [
        {
          sha: "deadbeef0000000000000000000000000000face",
          short_sha: "deadbee",
          subject: "checkpoint: pre-yolo refactor",
          author: "ak",
          ts: "2026-04-21T12:00:00Z",
        },
        {
          sha: "cafebabe0000000000000000000000000000face",
          short_sha: "cafebab",
          subject: "checkpoint: pre-yolo lint pass",
          author: "ak",
          ts: "2026-04-20T12:00:00Z",
        },
      ],
    };
    globalThis.fetch = buildFetchStub({
      checkpointsResponse: () =>
        new Response(JSON.stringify(populated), {
          status: 200,
          headers: { "content-type": "application/json" },
        }),
    });
    renderAt(`/s/${SESSION_NAME}/checkpoints`);

    expect(
      await screen.findByText(/pre-yolo refactor/i),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/pre-yolo lint pass/i),
    ).toBeInTheDocument();

    // Both sheets exist but are closed initially
    expect(screen.getByTestId("revert-sheet")).toHaveAttribute(
      "data-open",
      "0",
    );
    expect(screen.getByTestId("diff-sheet")).toHaveAttribute(
      "data-open",
      "0",
    );
  });

  it("renders an attention-state border when session.attention is non-clear", async () => {
    const attentive: Session = {
      ...baseSession,
      attention: {
        state: "permission_request",
        since: "2026-04-21T12:01:00Z",
      },
    };
    globalThis.fetch = buildFetchStub({
      sessionResponse: () =>
        new Response(JSON.stringify(attentive), {
          status: 200,
          headers: { "content-type": "application/json" },
        }),
    });
    renderAt(`/s/${SESSION_NAME}`);

    const region = await screen.findByRole("region", {
      name: `Session ${SESSION_NAME}`,
    });
    await waitFor(() => {
      expect(region).toHaveAttribute("data-attentive", "true");
    });
  });

  it("clicking the Feed tab navigates and switches the active tab", async () => {
    globalThis.fetch = buildFetchStub({});
    const user = userEvent.setup();
    renderAt(`/s/${SESSION_NAME}`);

    await screen.findByTestId("pane-stub");

    await user.click(screen.getByRole("tab", { name: "Feed" }));

    // After click, feed stub mounts and Feed tab is selected
    await waitFor(() => {
      expect(screen.getByRole("tab", { name: "Feed" })).toHaveAttribute(
        "aria-selected",
        "true",
      );
    });
    expect(screen.getByTestId("feed-stub")).toBeInTheDocument();
  });

  it("renders nothing when no :name param is in the URL", () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    // Render without a :name match — the route renders SessionDetail
    // directly with no params, exercising the early-return branch.
    const { container } = render(
      <QueryClientProvider client={qc}>
        <MemoryRouter initialEntries={["/standalone"]}>
          <Routes>
            <Route path="/standalone" element={<SessionDetail />} />
          </Routes>
        </MemoryRouter>
      </QueryClientProvider>,
    );
    expect(container.firstChild).toBeNull();
  });
});
