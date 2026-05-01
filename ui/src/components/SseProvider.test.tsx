import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, waitFor, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "@/components/AuthProvider";
import { SseProvider } from "@/components/SseProvider";
import { TOKEN_KEY } from "@/lib/api";
import type { Session } from "@/hooks/useSessions";
import type { ToolCallRow } from "@/hooks/useFeed";

/*
 * Mock fetch-event-source so we can:
 *   1. count subscribe / unsubscribe lifecycles
 *   2. drive onmessage / onerror by hand to exercise SseProvider's
 *      cache-mutation switch and the disconnect-grace timer.
 *
 * The real client never fires onmessage in this jsdom test — we own
 * the dispatch entirely. Each subscribe entry exposes its callbacks
 * back to the test.
 */
interface FesOpts {
  signal?: AbortSignal;
  onopen?: (r: Response) => void;
  onmessage?: (msg: { id: string; event: string; data: string }) => void;
  onerror?: (err: unknown) => number | void;
  onclose?: () => void;
}

interface SubEntry {
  url: string;
  aborted: boolean;
  opts: FesOpts;
}

const subscribed: SubEntry[] = [];

vi.mock("@microsoft/fetch-event-source", () => {
  return {
    fetchEventSource: vi.fn(async (url: string, opts: FesOpts) => {
      const entry: SubEntry = { url, aborted: false, opts };
      subscribed.push(entry);
      opts.signal?.addEventListener("abort", () => {
        entry.aborted = true;
      });
      // Simulate a successful open so onOpen fires (for the connected flag).
      await Promise.resolve();
      opts.onopen?.(
        new Response(null, {
          status: 200,
          headers: { "content-type": "text/event-stream" },
        }),
      );
      return new Promise(() => {
        /* never resolves; aborted via signal */
      });
    }),
  };
});

/**
 * The SseProvider only exposes `connected` via the context — we want
 * to assert against the cache after dispatching events. A small
 * helper keeps a handle on the QueryClient.
 */
function makeTree() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const ui = (
    <QueryClientProvider client={qc}>
      <AuthProvider>
        <SseProvider>
          <div>app</div>
        </SseProvider>
      </AuthProvider>
    </QueryClientProvider>
  );
  return { qc, ui };
}

/** Dispatch a typed SSE message into the most recent subscription. */
async function dispatch(type: string, data: unknown, id = "1-0") {
  const sub = subscribed[subscribed.length - 1];
  if (!sub) throw new Error("no active subscription");
  await act(async () => {
    sub.opts.onmessage?.({
      id,
      event: type,
      data: JSON.stringify(data),
    });
  });
}

describe("SseProvider", () => {
  beforeEach(() => {
    subscribed.length = 0;
    localStorage.clear();
  });

  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("subscribes once when a token is present and aborts on token change", async () => {
    localStorage.setItem(TOKEN_KEY, "token-1");
    const { ui } = makeTree();
    const { unmount } = render(ui);

    await waitFor(() => {
      expect(subscribed.length).toBe(1);
    });
    expect(subscribed[0].url).toBe("/events/all");
    expect(subscribed[0].aborted).toBe(false);

    // Simulate cross-tab token rotation via the storage event AuthProvider listens to.
    await act(async () => {
      localStorage.setItem(TOKEN_KEY, "token-2");
      window.dispatchEvent(
        new StorageEvent("storage", {
          key: TOKEN_KEY,
          newValue: "token-2",
          oldValue: "token-1",
        }),
      );
    });

    await waitFor(() => {
      expect(subscribed[0].aborted).toBe(true);
      expect(subscribed.length).toBeGreaterThanOrEqual(2);
    });

    unmount();
  });

  it("does not subscribe when no token is present (AuthGate path)", async () => {
    const { ui } = makeTree();
    render(ui);
    await new Promise((r) => setTimeout(r, 30));
    expect(subscribed.length).toBe(0);
  });

  it("upserts a new session into the ['sessions'] cache on session_new", async () => {
    localStorage.setItem(TOKEN_KEY, "t");
    const { qc, ui } = makeTree();
    render(ui);
    await waitFor(() => expect(subscribed.length).toBe(1));

    const s: Session = {
      name: "alpha",
      uuid: "u1",
      mode: "yolo",
      workdir: "/tmp",
      created_at: "2026-04-21T00:00:00Z",
      is_active: true,
      tmux_alive: true,
    };

    await dispatch("session_new", s);

    expect(qc.getQueryData<Session[]>(["sessions"])).toEqual([s]);
    expect(qc.getQueryData<Session>(["sessions", "alpha"])).toEqual(s);
  });

  it("merges into an existing row when session_attached fires for the same name", async () => {
    localStorage.setItem(TOKEN_KEY, "t");
    const { qc, ui } = makeTree();
    qc.setQueryData<Session[]>(
      ["sessions"],
      [
        {
          name: "alpha",
          uuid: "u1",
          mode: "yolo",
          workdir: "/tmp",
          created_at: "2026-04-21T00:00:00Z",
          is_active: false,
          tmux_alive: false,
        },
      ],
    );
    render(ui);
    await waitFor(() => expect(subscribed.length).toBe(1));

    await dispatch("session_attached", {
      name: "alpha",
      uuid: "u1",
      mode: "yolo",
      workdir: "/tmp",
      created_at: "2026-04-21T00:00:00Z",
      is_active: true,
      tmux_alive: true,
    });

    const list = qc.getQueryData<Session[]>(["sessions"]);
    expect(list).toHaveLength(1);
    expect(list?.[0].is_active).toBe(true);
    expect(list?.[0].tmux_alive).toBe(true);
  });

  it("drops a session and its detail cache on session_killed", async () => {
    localStorage.setItem(TOKEN_KEY, "t");
    const { qc, ui } = makeTree();
    qc.setQueryData<Session[]>(
      ["sessions"],
      [
        {
          name: "alpha",
          uuid: "u1",
          mode: "yolo",
          workdir: "/tmp",
          created_at: "2026-04-21T00:00:00Z",
          is_active: true,
          tmux_alive: true,
        },
      ],
    );
    qc.setQueryData(["sessions", "alpha"], { name: "alpha" });
    render(ui);
    await waitFor(() => expect(subscribed.length).toBe(1));

    await dispatch("session_killed", { name: "alpha" });

    expect(qc.getQueryData<Session[]>(["sessions"])).toEqual([]);
    expect(qc.getQueryData(["sessions", "alpha"])).toBeUndefined();
  });

  it("appends tool_call rows to ['feed','all'] and the per-session feed, stamping ev.id", async () => {
    localStorage.setItem(TOKEN_KEY, "t");
    const { qc, ui } = makeTree();
    render(ui);
    await waitFor(() => expect(subscribed.length).toBe(1));

    const row: ToolCallRow = {
      session: "alpha",
      tool: "Bash",
      input: "ls",
      summary: "ok",
      is_error: false,
      ts: "2026-04-21T16:00:00Z",
    };

    await dispatch("tool_call", row, "evt-9");

    const all = qc.getQueryData<ToolCallRow[]>(["feed", "all"]);
    const alpha = qc.getQueryData<ToolCallRow[]>(["feed", "alpha"]);
    expect(all).toHaveLength(1);
    expect(all?.[0].id).toBe("evt-9");
    expect(alpha).toHaveLength(1);
    expect(alpha?.[0].tool).toBe("Bash");
  });

  it("caps the global feed at 500 rows", async () => {
    localStorage.setItem(TOKEN_KEY, "t");
    const { qc, ui } = makeTree();
    // Pre-seed 499 rows so the next push tips us right at the FEED_CAP edge.
    const seed: ToolCallRow[] = Array.from({ length: 499 }, (_, i) => ({
      session: "alpha",
      tool: "Bash",
      input: `cmd-${i}`,
      summary: "ok",
      is_error: false,
      ts: `2026-04-21T16:00:${String(i).padStart(2, "0")}Z`,
    }));
    qc.setQueryData(["feed", "all"], seed);
    render(ui);
    await waitFor(() => expect(subscribed.length).toBe(1));

    // First push lands at 500 — still within cap.
    await dispatch(
      "tool_call",
      {
        session: "alpha",
        tool: "Bash",
        input: "cmd-499",
        summary: "ok",
        is_error: false,
        ts: "2026-04-21T17:00:00Z",
      },
      "id-499",
    );
    expect(qc.getQueryData<ToolCallRow[]>(["feed", "all"])).toHaveLength(500);

    // Second push trims the head — still 500, oldest evicted.
    await dispatch(
      "tool_call",
      {
        session: "alpha",
        tool: "Bash",
        input: "cmd-500",
        summary: "ok",
        is_error: false,
        ts: "2026-04-21T17:00:01Z",
      },
      "id-500",
    );
    const all = qc.getQueryData<ToolCallRow[]>(["feed", "all"])!;
    expect(all).toHaveLength(500);
    expect(all[0].input).toBe("cmd-1");
    expect(all[all.length - 1].input).toBe("cmd-500");
  });

  it("writes to ['quota'] when quota_update has no session", async () => {
    localStorage.setItem(TOKEN_KEY, "t");
    const { qc, ui } = makeTree();
    render(ui);
    await waitFor(() => expect(subscribed.length).toBe(1));

    await dispatch("quota_update", {
      window_started_at: "2026-04-21T15:00:00Z",
      window_resets_at: "2026-04-21T20:00:00Z",
      pct: 42,
    });

    expect(qc.getQueryData(["quota"])).toMatchObject({ pct: 42 });
  });

  it("patches per-session context_pct + tokens when quota_update carries a session", async () => {
    localStorage.setItem(TOKEN_KEY, "t");
    const { qc, ui } = makeTree();
    qc.setQueryData<Session[]>(
      ["sessions"],
      [
        {
          name: "alpha",
          uuid: "u1",
          mode: "yolo",
          workdir: "/tmp",
          created_at: "2026-04-21T00:00:00Z",
          is_active: true,
          tmux_alive: true,
        },
      ],
    );
    qc.setQueryData<Session>(["sessions", "alpha"], {
      name: "alpha",
      uuid: "u1",
      mode: "yolo",
      workdir: "/tmp",
      created_at: "2026-04-21T00:00:00Z",
      is_active: true,
      tmux_alive: true,
    });
    render(ui);
    await waitFor(() => expect(subscribed.length).toBe(1));

    await dispatch("quota_update", {
      session: "alpha",
      context_pct: 87,
      input_tokens: 100,
      output_tokens: 50,
      cache_tokens: 25,
    });

    const list = qc.getQueryData<Session[]>(["sessions"]);
    expect(list?.[0].context_pct).toBe(87);
    expect(list?.[0].tokens).toEqual({
      input_tokens: 100,
      output_tokens: 50,
      cache_tokens: 25,
    });
    const detail = qc.getQueryData<Session>(["sessions", "alpha"]);
    expect(detail?.context_pct).toBe(87);
  });

  it("ignores per-session quota_update when no relevant fields are present", async () => {
    localStorage.setItem(TOKEN_KEY, "t");
    const { qc, ui } = makeTree();
    const original: Session = {
      name: "alpha",
      uuid: "u1",
      mode: "yolo",
      workdir: "/tmp",
      created_at: "2026-04-21T00:00:00Z",
      is_active: true,
      tmux_alive: true,
      context_pct: 10,
    };
    qc.setQueryData<Session[]>(["sessions"], [original]);
    render(ui);
    await waitFor(() => expect(subscribed.length).toBe(1));

    await dispatch("quota_update", { session: "alpha" });

    // No-op patch — context_pct must remain 10.
    expect(qc.getQueryData<Session[]>(["sessions"])?.[0].context_pct).toBe(10);
  });

  it("writes ['attention', name] and patches the row on attention_raised", async () => {
    localStorage.setItem(TOKEN_KEY, "t");
    const { qc, ui } = makeTree();
    qc.setQueryData<Session[]>(
      ["sessions"],
      [
        {
          name: "alpha",
          uuid: "u1",
          mode: "yolo",
          workdir: "/tmp",
          created_at: "2026-04-21T00:00:00Z",
          is_active: true,
          tmux_alive: true,
        },
      ],
    );
    render(ui);
    await waitFor(() => expect(subscribed.length).toBe(1));

    await dispatch("attention_raised", {
      session: "alpha",
      state: "stalled",
      since: "2026-04-21T16:00:00Z",
    });

    expect(qc.getQueryData(["attention", "alpha"])).toMatchObject({
      state: "stalled",
    });
    expect(
      qc.getQueryData<Session[]>(["sessions"])?.[0].attention?.state,
    ).toBe("stalled");
  });

  it("clears attention on attention_cleared", async () => {
    localStorage.setItem(TOKEN_KEY, "t");
    const { qc, ui } = makeTree();
    qc.setQueryData(["attention", "alpha"], { state: "stalled" });
    qc.setQueryData<Session[]>(
      ["sessions"],
      [
        {
          name: "alpha",
          uuid: "u1",
          mode: "yolo",
          workdir: "/tmp",
          created_at: "2026-04-21T00:00:00Z",
          is_active: true,
          tmux_alive: true,
          attention: { state: "stalled" },
        },
      ],
    );
    render(ui);
    await waitFor(() => expect(subscribed.length).toBe(1));

    await dispatch("attention_cleared", { session: "alpha" });

    expect(qc.getQueryData(["attention", "alpha"])).toEqual({ state: "clear" });
    expect(
      qc.getQueryData<Session[]>(["sessions"])?.[0].attention?.state,
    ).toBe("clear");
  });

  it("invalidates subagent + team queries on subagent_start / subagent_stop / team_*", async () => {
    localStorage.setItem(TOKEN_KEY, "t");
    const { qc, ui } = makeTree();
    const spy = vi.spyOn(qc, "invalidateQueries");
    render(ui);
    await waitFor(() => expect(subscribed.length).toBe(1));

    await dispatch("subagent_start", { session: "alpha" });
    await dispatch("subagent_stop", { session: "alpha" });
    await dispatch("team_spawn", { session: "alpha" });
    await dispatch("team_settled", { session: "alpha" });

    // 2 invalidations per subagent_* event (subagents + teams) and 1 per team_*.
    const calls = spy.mock.calls.map((c) => c[0]?.queryKey);
    expect(calls).toEqual(
      expect.arrayContaining([
        ["subagents", "alpha"],
        ["teams", "alpha"],
      ]),
    );
    // 2 + 2 + 1 + 1 = 6 invalidations for these dispatches.
    expect(spy).toHaveBeenCalledTimes(6);
  });

  it("no-ops subagent / team events without a session field", async () => {
    localStorage.setItem(TOKEN_KEY, "t");
    const { qc, ui } = makeTree();
    const spy = vi.spyOn(qc, "invalidateQueries");
    render(ui);
    await waitFor(() => expect(subscribed.length).toBe(1));

    await dispatch("subagent_start", {});
    await dispatch("team_spawn", {});

    expect(spy).not.toHaveBeenCalled();
  });

  it("ignores unknown event types without throwing", async () => {
    localStorage.setItem(TOKEN_KEY, "t");
    const { qc, ui } = makeTree();
    render(ui);
    await waitFor(() => expect(subscribed.length).toBe(1));

    await dispatch("totally_made_up_event", { whatever: 1 });
    // Cache untouched.
    expect(qc.getQueryData(["sessions"])).toBeUndefined();
  });

  it("schedules a single disconnect timer on onerror and coalesces repeats", async () => {
    localStorage.setItem(TOKEN_KEY, "t");
    const setSpy = vi.spyOn(window, "setTimeout");
    const { ui } = makeTree();
    const { unmount } = render(ui);

    await waitFor(() => expect(subscribed.length).toBe(1));
    const sub = subscribed[0];

    setSpy.mockClear();
    // Two onerror calls in quick succession — only one 3s timer should
    // be scheduled (the second is coalesced).
    await act(async () => {
      sub.opts.onerror?.(new Error("transient"));
    });
    await act(async () => {
      sub.opts.onerror?.(new Error("again"));
    });

    // Filter to grace-window timers (3000ms) so we ignore React-internal
    // microtask scheduling.
    const graceTimers = setSpy.mock.calls.filter((c) => c[1] === 3000);
    expect(graceTimers).toHaveLength(1);

    setSpy.mockRestore();
    unmount();
  });

  it("clears the pending lost-timer when onopen recovers before the grace expires", async () => {
    localStorage.setItem(TOKEN_KEY, "t");
    const { ui } = makeTree();
    render(ui);

    await waitFor(() => expect(subscribed.length).toBe(1));
    const sub = subscribed[0];

    // Enter the grace window with onerror — capture the timer id that
    // gets returned so we can assert it is cleared.
    const setSpy = vi.spyOn(window, "setTimeout");
    const clearSpy = vi.spyOn(window, "clearTimeout");
    await act(async () => {
      sub.opts.onerror?.(new Error("blip"));
    });
    const graceCall = setSpy.mock.results.find(
      (_, i) => setSpy.mock.calls[i][1] === 3000,
    );
    expect(graceCall).toBeDefined();
    const timerId = graceCall!.value as number;

    // Recover via onopen — the grace timer must be cleared with the
    // matching id so it never fires.
    await act(async () => {
      sub.opts.onopen?.(
        new Response(null, {
          status: 200,
          headers: { "content-type": "text/event-stream" },
        }),
      );
    });
    expect(clearSpy).toHaveBeenCalledWith(timerId);

    setSpy.mockRestore();
    clearSpy.mockRestore();
  });

  it("clears the lost-timer on unmount", async () => {
    localStorage.setItem(TOKEN_KEY, "t");
    const { ui } = makeTree();
    const { unmount } = render(ui);
    await waitFor(() => expect(subscribed.length).toBe(1));
    const sub = subscribed[0];

    const setSpy = vi.spyOn(window, "setTimeout");
    const clearSpy = vi.spyOn(window, "clearTimeout");
    await act(async () => {
      sub.opts.onerror?.(new Error("blip"));
    });
    const graceCall = setSpy.mock.results.find(
      (_, i) => setSpy.mock.calls[i][1] === 3000,
    );
    expect(graceCall).toBeDefined();
    const timerId = graceCall!.value as number;

    // Unmount mid-grace — the cleanup effect must clear the timer.
    unmount();
    expect(clearSpy).toHaveBeenCalledWith(timerId);

    setSpy.mockRestore();
    clearSpy.mockRestore();
  });
});

describe("useSseStatus", () => {
  beforeEach(() => {
    subscribed.length = 0;
    localStorage.clear();
  });

  it("returns connected:false outside the provider (default context value)", async () => {
    const mod = await import("@/components/SseProvider");
    // The hook is just a useContext wrapper; calling outside React is
    // not its contract — instead, render a consumer inside a tree
    // without the provider mounted.
    function Consumer() {
      const status = mod.useSseStatus();
      return <span data-testid="status">{String(status.connected)}</span>;
    }
    const { getByTestId } = render(<Consumer />);
    expect(getByTestId("status").textContent).toBe("false");
  });
});
