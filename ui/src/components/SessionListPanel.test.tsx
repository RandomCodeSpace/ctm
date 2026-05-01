import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { SessionListPanel } from "@/components/SessionListPanel";
import type { Session } from "@/hooks/useSessions";
import { TOKEN_KEY } from "@/lib/api";

/** Make a baseline session — overrides win. */
function makeSession(overrides: Partial<Session> = {}): Session {
  return {
    name: "alpha",
    uuid: "00000000-0000-0000-0000-000000000001",
    mode: "yolo",
    workdir: "/tmp/alpha",
    created_at: new Date(Date.now() - 60_000).toISOString(),
    last_attached_at: new Date(Date.now() - 30_000).toISOString(),
    last_tool_call_at: new Date(Date.now() - 5_000).toISOString(),
    is_active: true,
    tmux_alive: true,
    ...overrides,
  };
}

/** Stub /api/sessions — single-shot deterministic response. */
function stubSessionsResponse(
  body: unknown,
  init: { status?: number } = {},
) {
  globalThis.fetch = vi.fn(async () =>
    new Response(JSON.stringify(body), {
      status: init.status ?? 200,
      headers: { "content-type": "application/json" },
    }),
  ) as unknown as typeof globalThis.fetch;
}

function renderPanel(activeName?: string) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter>
        <SessionListPanel activeName={activeName} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("SessionListPanel", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    // SessionCard formatting needs the user-visible "active" state —
    // a token isn't strictly required for the panel itself, but the
    // api() helper still injects a header when one's present. Keep
    // localStorage clean.
    localStorage.setItem(TOKEN_KEY, "test-token");
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("shows skeletons while the sessions query is in flight", async () => {
    // Hold the response open so isLoading stays true.
    let resolve!: (r: Response) => void;
    globalThis.fetch = vi.fn(
      () => new Promise<Response>((r) => (resolve = r)),
    ) as unknown as typeof globalThis.fetch;

    const { container } = renderPanel();
    // Three skeleton blocks per the source — they don't have role,
    // so query by the design-system class signature.
    const skeletons = container.querySelectorAll(".h-16.w-full");
    expect(skeletons.length).toBe(3);

    // Drain the in-flight request so React Query unmounts cleanly.
    resolve(
      new Response("[]", {
        status: 200,
        headers: { "content-type": "application/json" },
      }),
    );
    await waitFor(() => {
      expect(screen.getByText(/No active sessions/i)).toBeInTheDocument();
    });
  });

  it("renders the empty-active message when no sessions are active", async () => {
    stubSessionsResponse([]);
    renderPanel();

    await waitFor(() => {
      expect(
        screen.getByText(/No active sessions\. Start one with ctm new/i),
      ).toBeInTheDocument();
    });
  });

  it("renders a card per active session and applies the active highlight", async () => {
    stubSessionsResponse([
      makeSession({ name: "alpha" }),
      makeSession({
        name: "beta",
        uuid: "00000000-0000-0000-0000-000000000002",
        workdir: "/tmp/beta",
      }),
    ]);
    renderPanel("alpha");

    await waitFor(() => {
      expect(screen.getByText("alpha")).toBeInTheDocument();
    });
    expect(screen.getByText("beta")).toBeInTheDocument();

    // Active card carries aria-current="page" (set by SessionCard's
    // <Link>). Use that as the contract — the visual treatment is
    // the SessionCard's concern, not the panel's.
    const links = screen.getAllByRole("link");
    const activeLink = links.find(
      (l) => l.getAttribute("aria-current") === "page",
    );
    expect(activeLink).toBeDefined();
    expect(activeLink?.textContent).toContain("alpha");
  });

  it("filters out inactive sessions until 'Show all' is checked", async () => {
    stubSessionsResponse([
      makeSession({ name: "alpha", is_active: true }),
      makeSession({
        name: "ghost",
        uuid: "00000000-0000-0000-0000-000000000099",
        is_active: false,
        tmux_alive: false,
      }),
    ]);
    const user = userEvent.setup();
    renderPanel();

    await waitFor(() => {
      expect(screen.getByText("alpha")).toBeInTheDocument();
    });
    expect(screen.queryByText("ghost")).not.toBeInTheDocument();

    await user.click(screen.getByRole("checkbox", { name: /show all/i }));
    expect(screen.getByText("ghost")).toBeInTheDocument();
    // Active still visible.
    expect(screen.getByText("alpha")).toBeInTheDocument();
  });

  it("shows the 'no sessions on record' message when 'Show all' is on and the list is empty", async () => {
    stubSessionsResponse([]);
    const user = userEvent.setup();
    renderPanel();

    await waitFor(() => {
      expect(screen.getByText(/No active sessions/i)).toBeInTheDocument();
    });

    await user.click(screen.getByRole("checkbox", { name: /show all/i }));
    expect(screen.getByText(/No sessions on record\./i)).toBeInTheDocument();
  });

  it("renders the alert region with the error message when the query fails", async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response(JSON.stringify({ error: "boom" }), {
        status: 500,
        headers: { "content-type": "application/json" },
      }),
    ) as unknown as typeof globalThis.fetch;

    renderPanel();

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent(/Could not load sessions/i);
  });

  it("renders the Live feed footer link to /feed", async () => {
    stubSessionsResponse([]);
    renderPanel();

    const link = await screen.findByRole("link", { name: /live feed/i });
    expect(link).toHaveAttribute("href", "/feed");
  });

  it("exposes the panel as an aside with an accessible label", async () => {
    stubSessionsResponse([]);
    renderPanel();
    const aside = screen.getByRole("complementary", { name: /sessions/i });
    expect(aside.tagName).toBe("ASIDE");
  });
});
