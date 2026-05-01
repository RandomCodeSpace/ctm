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
import type { ReactNode } from "react";
import { TOKEN_KEY } from "@/lib/api";

/*
 * App.test.tsx — integration test of the top-level shell.
 *
 * App.tsx wires the global providers (Theme -> design-system bridge ->
 * QueryClient -> Auth -> Sse -> AuthGate) and a `createBrowserRouter`.
 * For tests we:
 *
 *   1. drive route resolution through `window.history.pushState` rather
 *      than a MemoryRouter — RouterProvider takes a router instance, so
 *      we have to use the real (browser) history API the createBrowserRouter
 *      reads from. jsdom implements it.
 *   2. mock the heavy children (Dashboard, DoctorPanel, FeedFullscreen,
 *      SessionDetail) — those have their own dedicated suites and pull in
 *      SSE/network on mount, which would explode in this shell-level test.
 *   3. mock SseProvider so we don't actually open EventSource / use
 *      fetch-event-source. We expose a fake `useSseStatus` so
 *      ConnectionBanner can read the connected flag the same way it
 *      does in production.
 *   4. stub `globalThis.fetch` for /api/auth/status so AuthGate resolves
 *      either to <LoginForm> (unauthenticated) or to <App children>
 *      (authenticated). All other endpoints return 404 — the route
 *      stubs never call them.
 */

vi.mock("@/components/SseProvider", () => {
  let connected = true;
  return {
    SseProvider: ({ children }: { children: ReactNode }) => (
      <div data-testid="sse-provider-stub">{children}</div>
    ),
    useSseStatus: () => ({ connected }),
    /** Test-only escape hatch — flip the SSE banner state. */
    __setSseConnected: (v: boolean) => {
      connected = v;
    },
  };
});

vi.mock("@/routes/Dashboard", () => ({
  Dashboard: () => <div data-testid="dashboard-stub">dashboard</div>,
}));

vi.mock("@/routes/DoctorPanel", () => ({
  DoctorPanel: () => <div data-testid="doctor-stub">doctor</div>,
}));

vi.mock("@/routes/FeedFullscreen", () => ({
  FeedFullscreen: () => <div data-testid="feed-fullscreen-stub">feed-fs</div>,
}));

// LoginForm renders a real <form> — much lighter than a stub here, but
// we lean on a stub so we don't need to wire useLogin's mutation path.
vi.mock("@/routes/LoginForm", () => ({
  LoginForm: ({ onSwitchToSignup }: { onSwitchToSignup?: () => void }) => (
    <div data-testid="login-stub">
      <span>login</span>
      {onSwitchToSignup && (
        <button onClick={onSwitchToSignup} aria-label="switch-to-signup">
          to-signup
        </button>
      )}
    </div>
  ),
}));

vi.mock("@/routes/SignupForm", () => ({
  SignupForm: ({ onSwitchToLogin }: { onSwitchToLogin?: () => void }) => (
    <div data-testid="signup-stub">
      <span>signup</span>
      {onSwitchToLogin && (
        <button onClick={onSwitchToLogin} aria-label="switch-to-login">
          to-login
        </button>
      )}
    </div>
  ),
}));

// design-system pulls in real CSS in App.tsx — the bridge component
// only needs ToastRegion + ThemeProvider. Stub them so jsdom doesn't
// have to parse design-system styles.
vi.mock("@ossrandom/design-system", () => ({
  ThemeProvider: ({
    mode,
    children,
  }: {
    mode: "light" | "dark";
    children: ReactNode;
  }) => (
    <div data-testid="ds-theme" data-mode={mode}>
      {children}
    </div>
  ),
  ToastRegion: () => <div data-testid="toast-region" />,
}));

// design-system styles import — vite/vitest treats `.css` as opaque
// modules in node, but the bare-specifier resolution needs a stub so
// the dynamic import doesn't try to walk into the package.
vi.mock("@ossrandom/design-system/styles.css", () => ({}));

// Lazy import so the vi.mock factories above are hoisted before the
// module under test pulls them in.
async function loadApp() {
  const mod = await import("@/App");
  return mod.App;
}

interface AuthStatus {
  registered: boolean;
  authenticated: boolean;
}

interface FetchState {
  authStatus?: AuthStatus | "error";
}

function buildFetchStub(state: FetchState = {}): typeof globalThis.fetch {
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = typeof input === "string" ? input : input.toString();
    if (url.includes("/api/auth/status")) {
      if (state.authStatus === "error") {
        return new Response("boom", { status: 500 });
      }
      const body: AuthStatus = state.authStatus ?? {
        registered: true,
        authenticated: true,
      };
      return new Response(JSON.stringify(body), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    }
    return new Response("not found", { status: 404 });
  }) as unknown as typeof globalThis.fetch;
}

/**
 * createBrowserRouter reads from window.history. Reset the URL to a
 * fresh path before each test so route assertions are deterministic.
 */
function navigateTo(path: string) {
  window.history.pushState({}, "", path);
}

describe("App", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    localStorage.setItem(TOKEN_KEY, "test-token");

    // jsdom doesn't implement matchMedia. ThemeProvider uses it to
    // resolve "system" preference into light/dark.
    if (!window.matchMedia) {
      Object.defineProperty(window, "matchMedia", {
        writable: true,
        configurable: true,
        value: (query: string) => ({
          matches: false,
          media: query,
          onchange: null,
          addListener: vi.fn(),
          removeListener: vi.fn(),
          addEventListener: vi.fn(),
          removeEventListener: vi.fn(),
          dispatchEvent: vi.fn(),
        }),
      });
    }
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    localStorage.clear();
    // Walk back to "/" so the next test's createBrowserRouter
    // initialises at a known path.
    window.history.pushState({}, "", "/");
    vi.restoreAllMocks();
    // vi.resetModules so each test re-imports App with a fresh
    // createBrowserRouter (its `router` constant is module-level).
    vi.resetModules();
  });

  it("renders Dashboard at /", async () => {
    globalThis.fetch = buildFetchStub();
    navigateTo("/");
    const App = await loadApp();
    render(<App />);
    expect(await screen.findByTestId("dashboard-stub")).toBeInTheDocument();
    // Provider tree wired around it.
    expect(screen.getByTestId("ds-theme")).toHaveAttribute("data-mode", "dark");
    expect(screen.getByTestId("toast-region")).toBeInTheDocument();
    expect(screen.getByTestId("sse-provider-stub")).toBeInTheDocument();
  });

  it("renders Dashboard at /s/:name (session route)", async () => {
    globalThis.fetch = buildFetchStub();
    navigateTo("/s/alpha");
    const App = await loadApp();
    render(<App />);
    expect(await screen.findByTestId("dashboard-stub")).toBeInTheDocument();
  });

  it("renders Dashboard at the /s/:name/* tab variants", async () => {
    globalThis.fetch = buildFetchStub();
    for (const path of [
      "/s/alpha/feed",
      "/s/alpha/checkpoints",
      "/s/alpha/pane",
      "/s/alpha/subagents",
      "/s/alpha/teams",
      "/s/alpha/meta",
    ]) {
      navigateTo(path);
      const App = await loadApp();
      const { unmount } = render(<App />);
      expect(await screen.findByTestId("dashboard-stub")).toBeInTheDocument();
      unmount();
      vi.resetModules();
    }
  });

  it("renders FeedFullscreen at /feed", async () => {
    globalThis.fetch = buildFetchStub();
    navigateTo("/feed");
    const App = await loadApp();
    render(<App />);
    expect(
      await screen.findByTestId("feed-fullscreen-stub"),
    ).toBeInTheDocument();
  });

  it("renders DoctorPanel at /doctor", async () => {
    globalThis.fetch = buildFetchStub();
    navigateTo("/doctor");
    const App = await loadApp();
    render(<App />);
    expect(await screen.findByTestId("doctor-stub")).toBeInTheDocument();
  });

  it("matches the catchall route for unknown URLs without throwing", async () => {
    globalThis.fetch = buildFetchStub();
    navigateTo("/this/does/not/exist");
    const App = await loadApp();
    // The router's `*` route uses <Navigate to="/" replace />. We don't
    // assert on the URL change because react-router v7 + jsdom don't
    // settle the redirect reliably inside a test render. We DO assert
    // the App didn't crash and the providers still mounted — i.e., the
    // catch-all branch in the routes array is exercised without an
    // unhandled error from a missing match.
    expect(() => render(<App />)).not.toThrow();
    await waitFor(() => {
      expect(screen.getByTestId("sse-provider-stub")).toBeInTheDocument();
    });
  });

  it("renders LoginForm when the daemon reports registered & unauthenticated", async () => {
    globalThis.fetch = buildFetchStub({
      authStatus: { registered: true, authenticated: false },
    });
    navigateTo("/");
    const App = await loadApp();
    render(<App />);
    expect(await screen.findByTestId("login-stub")).toBeInTheDocument();
    // Dashboard is gated out.
    expect(screen.queryByTestId("dashboard-stub")).not.toBeInTheDocument();
  });

  it("renders SignupForm when the daemon reports no registered user", async () => {
    globalThis.fetch = buildFetchStub({
      authStatus: { registered: false, authenticated: false },
    });
    navigateTo("/");
    const App = await loadApp();
    render(<App />);
    expect(await screen.findByTestId("signup-stub")).toBeInTheDocument();
    expect(screen.queryByTestId("dashboard-stub")).not.toBeInTheDocument();
  });

  it("AuthGate user can switch from signup to login via the override", async () => {
    globalThis.fetch = buildFetchStub({
      authStatus: { registered: false, authenticated: false },
    });
    navigateTo("/");
    const App = await loadApp();
    render(<App />);

    expect(await screen.findByTestId("signup-stub")).toBeInTheDocument();
    const user = userEvent.setup();
    await user.click(
      screen.getByRole("button", { name: /switch-to-login/i }),
    );
    expect(await screen.findByTestId("login-stub")).toBeInTheDocument();
  });

  it("AuthGate shows a daemon-error banner when /api/auth/status fails", async () => {
    globalThis.fetch = buildFetchStub({ authStatus: "error" });
    navigateTo("/");
    const App = await loadApp();
    render(<App />);
    expect(
      await screen.findByText(/could not reach the daemon/i),
    ).toBeInTheDocument();
    expect(screen.queryByTestId("dashboard-stub")).not.toBeInTheDocument();
  });

  it("ConnectionBanner is hidden when SSE is connected", async () => {
    globalThis.fetch = buildFetchStub();
    navigateTo("/");
    const App = await loadApp();
    render(<App />);
    await screen.findByTestId("dashboard-stub");
    expect(
      screen.queryByText(/connection lost/i),
    ).not.toBeInTheDocument();
  });

  it("ConnectionBanner surfaces when SSE drops", async () => {
    const sse = await import("@/components/SseProvider");
    (sse as unknown as { __setSseConnected: (v: boolean) => void }).__setSseConnected(
      false,
    );
    globalThis.fetch = buildFetchStub();
    navigateTo("/");
    const App = await loadApp();
    render(<App />);
    await screen.findByTestId("dashboard-stub");
    expect(screen.getByText(/connection lost/i)).toBeInTheDocument();
    // Reset so other tests see a connected SSE.
    (sse as unknown as { __setSseConnected: (v: boolean) => void }).__setSseConnected(
      true,
    );
  });
});
