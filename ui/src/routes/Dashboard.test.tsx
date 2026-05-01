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
import { MemoryRouter, Route, Routes, useLocation } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Dashboard } from "@/routes/Dashboard";
import type { Session } from "@/hooks/useSessions";
import { TOKEN_KEY } from "@/lib/api";

/*
 * Tests focus on Dashboard's own contract:
 *   - layout chrome (header buttons, settings, new session, theme toggle)
 *   - empty / loading / populated states for the session list
 *   - desktop auto-navigate to the top active session
 *   - click-through opens settings / new session modals
 *   - SessionDetail mount when :name is present, EmptyDetail otherwise
 *
 * All sub-components that fetch / SSE on their own are mocked (they each
 * have their own dedicated suites), keeping this a unit test of the
 * route shell.
 */

vi.mock("@/components/CostChart", () => ({
  CostChart: () => <div data-testid="cost-stub" />,
}));

vi.mock("@/components/QuotaStrip", () => ({
  QuotaStrip: () => <div data-testid="quota-stub" />,
}));

vi.mock("@/components/SessionListPanel", () => ({
  SessionListPanel: ({
    activeName,
    className,
  }: {
    activeName?: string;
    className?: string;
  }) => (
    <div
      data-testid="session-list-stub"
      data-active={activeName ?? ""}
      data-class={className ?? ""}
    >
      list
    </div>
  ),
}));

vi.mock("@/components/SettingsDrawer", () => ({
  SettingsDrawer: ({
    open,
    onClose,
  }: {
    open: boolean;
    onClose: () => void;
  }) =>
    open ? (
      <div data-testid="settings-drawer">
        <button onClick={onClose} aria-label="close-settings">
          close
        </button>
      </div>
    ) : null,
}));

vi.mock("@/components/NewSessionModal", () => ({
  NewSessionModal: ({
    open,
    onClose,
    recents,
  }: {
    open: boolean;
    onClose: () => void;
    recents: string[];
  }) =>
    open ? (
      <div data-testid="new-session-modal" data-recents={recents.length}>
        <button onClick={onClose} aria-label="close-new-session">
          close
        </button>
      </div>
    ) : null,
}));

vi.mock("@/components/ThemeToggle", () => ({
  ThemeToggle: () => <div data-testid="theme-toggle" />,
}));

vi.mock("@/routes/SessionDetail", () => ({
  SessionDetail: ({ embedded }: { embedded?: boolean }) => (
    <div data-testid="session-detail-stub" data-embedded={embedded ? "1" : "0"}>
      session-detail
    </div>
  ),
}));

vi.mock("@/hooks/useRecentWorkdirs", () => ({
  useRecentWorkdirs: () => ["/tmp/a", "/tmp/b"],
}));

const matchMediaMatches = { value: false };

function makeSession(overrides: Partial<Session> = {}): Session {
  return {
    name: "alpha",
    uuid: "11111111-2222-3333-4444-555555555555",
    mode: "yolo",
    workdir: "/home/dev/projects/ctm",
    created_at: "2026-04-01T10:00:00Z",
    last_attached_at: "2026-04-21T12:00:00Z",
    last_tool_call_at: "2026-04-21T12:05:00Z",
    is_active: true,
    tmux_alive: true,
    ...overrides,
  };
}

interface FetchState {
  sessionsResponse?: () => Response | Promise<Response>;
}

function buildFetchStub(state: FetchState = {}): typeof globalThis.fetch {
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    if (url.includes("/api/sessions")) {
      return state.sessionsResponse
        ? state.sessionsResponse()
        : new Response(JSON.stringify([]), {
            status: 200,
            headers: { "content-type": "application/json" },
          });
    }
    return new Response("not found", { status: 404 });
  }) as unknown as typeof globalThis.fetch;
}

/**
 * Captures the current URL so we can assert auto-navigate behaviour
 * without a real browser history.
 */
function LocationSpy({ onLocation }: { onLocation: (path: string) => void }) {
  const loc = useLocation();
  onLocation(loc.pathname);
  return null;
}

function renderAt(
  path: string,
  onLocation: (p: string) => void = () => {},
) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <MemoryRouter initialEntries={[path]}>
        <Routes>
          <Route path="/" element={<Dashboard />} />
          <Route path="/s/:name" element={<Dashboard />} />
          <Route path="/doctor" element={<div data-testid="doctor-route" />} />
        </Routes>
        <LocationSpy onLocation={onLocation} />
      </MemoryRouter>
    </QueryClientProvider>,
  );
}

describe("Dashboard", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    localStorage.setItem(TOKEN_KEY, "test-token");
    matchMediaMatches.value = false;
    // jsdom does not implement matchMedia. Dashboard uses it to gate the
    // desktop auto-navigate behaviour, so we install a stub whose
    // `matches` value the test can flip per-case.
    Object.defineProperty(window, "matchMedia", {
      writable: true,
      configurable: true,
      value: (query: string) => ({
        matches: matchMediaMatches.value,
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      }),
    });
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("renders the page header chrome and quota strip on the root path", async () => {
    globalThis.fetch = buildFetchStub();
    renderAt("/");

    expect(
      await screen.findByRole("heading", { name: /ctm/i }),
    ).toBeInTheDocument();
    expect(screen.getByText(/claude tmux manager/i)).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /open doctor diagnostics/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /new session/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: /open settings/i }),
    ).toBeInTheDocument();
    expect(screen.getByTestId("theme-toggle")).toBeInTheDocument();
    expect(screen.getByTestId("quota-stub")).toBeInTheDocument();
    // CostChart is desktop-only via `hidden md:block` — but it still mounts.
    expect(screen.getByTestId("cost-stub")).toBeInTheDocument();
  });

  it("renders SessionListPanel and the EmptyDetail copy when no :name is selected (mobile)", async () => {
    matchMediaMatches.value = false; // mobile -> no auto-navigate
    globalThis.fetch = buildFetchStub({
      sessionsResponse: () =>
        new Response(JSON.stringify([makeSession()]), {
          status: 200,
          headers: { "content-type": "application/json" },
        }),
    });
    renderAt("/");

    expect(await screen.findByTestId("session-list-stub")).toBeInTheDocument();
    // EmptyDetail copy
    expect(screen.getByText(/no session selected/i)).toBeInTheDocument();
    expect(
      screen.getByText(/pick a session from the list/i),
    ).toBeInTheDocument();
    // SessionDetail not mounted
    expect(screen.queryByTestId("session-detail-stub")).not.toBeInTheDocument();
  });

  it("mounts SessionDetail (embedded) when a :name route is active", async () => {
    globalThis.fetch = buildFetchStub();
    renderAt("/s/alpha");

    const detail = await screen.findByTestId("session-detail-stub");
    expect(detail).toHaveAttribute("data-embedded", "1");
    // Active session forwarded to the list pane
    expect(screen.getByTestId("session-list-stub")).toHaveAttribute(
      "data-active",
      "alpha",
    );
  });

  it("does not auto-navigate on the root path when sessions list is empty", async () => {
    matchMediaMatches.value = true; // desktop
    globalThis.fetch = buildFetchStub({
      sessionsResponse: () =>
        new Response(JSON.stringify([]), {
          status: 200,
          headers: { "content-type": "application/json" },
        }),
    });
    let lastPath = "";
    renderAt("/", (p) => {
      lastPath = p;
    });

    // Wait for the sessions query to resolve so the effect would run.
    await waitFor(() => {
      expect(screen.getByTestId("session-list-stub")).toBeInTheDocument();
    });
    // No sessions -> no navigation. Still on "/".
    expect(lastPath).toBe("/");
    expect(screen.queryByTestId("session-detail-stub")).not.toBeInTheDocument();
  });

  it("does NOT auto-navigate on mobile (matchMedia min-width:768px = false)", async () => {
    matchMediaMatches.value = false; // mobile
    globalThis.fetch = buildFetchStub({
      sessionsResponse: () =>
        new Response(JSON.stringify([makeSession()]), {
          status: 200,
          headers: { "content-type": "application/json" },
        }),
    });
    let lastPath = "";
    renderAt("/", (p) => {
      lastPath = p;
    });

    // Give the effect a chance to run after the query settles.
    await waitFor(() => {
      expect(screen.getByTestId("session-list-stub")).toBeInTheDocument();
    });
    // The effect runs but bails on the matchMedia check. Stay on "/".
    expect(lastPath).toBe("/");
    expect(screen.queryByTestId("session-detail-stub")).not.toBeInTheDocument();
  });

  it("auto-navigates on desktop to the top active session ordered by activity", async () => {
    matchMediaMatches.value = true; // desktop
    const older = makeSession({
      name: "older",
      last_tool_call_at: "2026-04-20T10:00:00Z",
    });
    const newer = makeSession({
      name: "newer",
      last_tool_call_at: "2026-04-21T18:00:00Z",
    });
    const inactive = makeSession({
      name: "inactive",
      is_active: false,
      last_tool_call_at: "2026-04-25T18:00:00Z",
    });
    globalThis.fetch = buildFetchStub({
      sessionsResponse: () =>
        new Response(JSON.stringify([older, inactive, newer]), {
          status: 200,
          headers: { "content-type": "application/json" },
        }),
    });
    let lastPath = "";
    renderAt("/", (p) => {
      lastPath = p;
    });

    await waitFor(() => {
      expect(lastPath).toBe("/s/newer");
    });
    expect(screen.getByTestId("session-detail-stub")).toHaveAttribute(
      "data-embedded",
      "1",
    );
  });

  it("does NOT auto-navigate when a :name is already in the URL", async () => {
    matchMediaMatches.value = true; // desktop
    globalThis.fetch = buildFetchStub({
      sessionsResponse: () =>
        new Response(JSON.stringify([makeSession({ name: "different" })]), {
          status: 200,
          headers: { "content-type": "application/json" },
        }),
    });
    let lastPath = "";
    renderAt("/s/alpha", (p) => {
      lastPath = p;
    });
    await waitFor(() => {
      expect(screen.getByTestId("session-detail-stub")).toBeInTheDocument();
    });
    // Stays on /s/alpha — early-return branch for `if (name) return`.
    expect(lastPath).toBe("/s/alpha");
  });

  it("opens and closes the settings drawer", async () => {
    globalThis.fetch = buildFetchStub();
    const user = userEvent.setup();
    renderAt("/");

    expect(screen.queryByTestId("settings-drawer")).not.toBeInTheDocument();
    await user.click(
      screen.getByRole("button", { name: /open settings/i }),
    );
    expect(screen.getByTestId("settings-drawer")).toBeInTheDocument();

    await user.click(
      screen.getByRole("button", { name: /close-settings/i }),
    );
    expect(screen.queryByTestId("settings-drawer")).not.toBeInTheDocument();
  });

  it("opens the new-session modal and forwards recent workdirs", async () => {
    globalThis.fetch = buildFetchStub();
    const user = userEvent.setup();
    renderAt("/");

    expect(screen.queryByTestId("new-session-modal")).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: /new session/i }));
    const modal = screen.getByTestId("new-session-modal");
    expect(modal).toBeInTheDocument();
    expect(modal).toHaveAttribute("data-recents", "2");

    await user.click(
      screen.getByRole("button", { name: /close-new-session/i }),
    );
    expect(screen.queryByTestId("new-session-modal")).not.toBeInTheDocument();
  });

  it("navigates to /doctor when the diagnostics button is clicked", async () => {
    globalThis.fetch = buildFetchStub();
    const user = userEvent.setup();
    let lastPath = "";
    renderAt("/", (p) => {
      lastPath = p;
    });

    await user.click(
      screen.getByRole("button", { name: /open doctor diagnostics/i }),
    );
    await waitFor(() => {
      expect(lastPath).toBe("/doctor");
    });
    expect(screen.getByTestId("doctor-route")).toBeInTheDocument();
  });
});
