import {
  afterEach,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { usePaneStream } from "@/hooks/usePaneStream";
import { TOKEN_KEY, UnauthorizedError } from "@/lib/api";

/*
 * Mock @microsoft/fetch-event-source — same pattern as
 * SseProvider.test.tsx. We keep one entry per subscribe call so the
 * test can flip onopen / onmessage / onerror by hand. The real
 * fetch-event-source machinery (real network, EventSource) is not
 * runnable inside jsdom, so it's intentionally not exercised here —
 * we test the hook's contract: state transitions in response to
 * library callbacks, and abort-on-unmount cleanup.
 */
interface FesOpts {
  signal?: AbortSignal;
  headers?: Record<string, string>;
  openWhenHidden?: boolean;
  onopen?: (r: Response) => void | Promise<void>;
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

vi.mock("@microsoft/fetch-event-source", () => ({
  fetchEventSource: vi.fn(async (url: string, opts: FesOpts) => {
    const entry: SubEntry = { url, aborted: false, opts };
    subscribed.push(entry);
    opts.signal?.addEventListener("abort", () => {
      entry.aborted = true;
    });
    // Don't auto-open — let the test drive onopen explicitly so it can
    // exercise both happy + 401 paths.
    return new Promise(() => {
      /* never resolves; aborted via signal */
    });
  }),
}));

/** Most-recent subscribe entry. */
function lastSub(): SubEntry {
  const sub = subscribed[subscribed.length - 1];
  if (!sub) throw new Error("no active subscription");
  return sub;
}

describe("usePaneStream", () => {
  beforeEach(() => {
    subscribed.length = 0;
    localStorage.setItem(TOKEN_KEY, "test-token");
  });

  afterEach(() => {
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("returns the initial empty state and does not subscribe when disabled", () => {
    const { result } = renderHook(() => usePaneStream("alpha", false));
    expect(result.current).toEqual({
      text: "",
      connected: false,
      ended: false,
    });
    expect(subscribed.length).toBe(0);
  });

  it("does not subscribe when sessionName is undefined", () => {
    const { result } = renderHook(() => usePaneStream(undefined, true));
    expect(result.current).toEqual({
      text: "",
      connected: false,
      ended: false,
    });
    expect(subscribed.length).toBe(0);
  });

  it("subscribes to /events/session/<name>/pane with auth + accept headers", async () => {
    renderHook(() => usePaneStream("alpha", true));
    await waitFor(() => expect(subscribed.length).toBe(1));

    const sub = lastSub();
    expect(sub.url).toBe("/events/session/alpha/pane");
    expect(sub.opts.headers).toMatchObject({
      Authorization: "Bearer test-token",
      Accept: "text/event-stream",
    });
    expect(sub.opts.openWhenHidden).toBe(true);
  });

  it("encodes the session name into the URL path", async () => {
    renderHook(() => usePaneStream("a/b c", true));
    await waitFor(() => expect(subscribed.length).toBe(1));
    expect(lastSub().url).toBe(
      `/events/session/${encodeURIComponent("a/b c")}/pane`,
    );
  });

  it("flips connected=true after a successful onopen", async () => {
    const { result } = renderHook(() => usePaneStream("alpha", true));
    await waitFor(() => expect(subscribed.length).toBe(1));
    expect(result.current.connected).toBe(false);

    await act(async () => {
      await lastSub().opts.onopen?.(
        new Response(null, {
          status: 200,
          headers: { "content-type": "text/event-stream" },
        }),
      );
    });
    expect(result.current.connected).toBe(true);
  });

  it("throws UnauthorizedError on 401 in onopen and leaves connected=false", async () => {
    const { result } = renderHook(() => usePaneStream("alpha", true));
    await waitFor(() => expect(subscribed.length).toBe(1));

    await act(async () => {
      try {
        await lastSub().opts.onopen?.(
          new Response(null, { status: 401 }),
        );
        // Should not reach here.
        throw new Error("expected onopen to throw on 401");
      } catch (err) {
        expect(err).toBeInstanceOf(UnauthorizedError);
      }
    });
    expect(result.current.connected).toBe(false);
  });

  it("throws a generic Error on non-2xx non-401 in onopen", async () => {
    renderHook(() => usePaneStream("alpha", true));
    await waitFor(() => expect(subscribed.length).toBe(1));

    await act(async () => {
      try {
        await lastSub().opts.onopen?.(
          new Response(null, { status: 503 }),
        );
        throw new Error("expected onopen to throw on 503");
      } catch (err) {
        expect(err).toBeInstanceOf(Error);
        expect(err).not.toBeInstanceOf(UnauthorizedError);
        expect((err as Error).message).toContain("503");
      }
    });
  });

  it("updates text from a 'pane' event with a JSON-encoded string payload", async () => {
    const { result } = renderHook(() => usePaneStream("alpha", true));
    await waitFor(() => expect(subscribed.length).toBe(1));

    const payload = "hello [31mworld[0m";
    await act(async () => {
      lastSub().opts.onmessage?.({
        id: "1",
        event: "pane",
        data: JSON.stringify(payload),
      });
    });
    expect(result.current.text).toBe(payload);
    expect(result.current.ended).toBe(false);
  });

  it("falls back to raw data when the 'pane' payload is not valid JSON", async () => {
    const { result } = renderHook(() => usePaneStream("alpha", true));
    await waitFor(() => expect(subscribed.length).toBe(1));

    await act(async () => {
      lastSub().opts.onmessage?.({
        id: "2",
        event: "pane",
        data: "not-json{",
      });
    });
    expect(result.current.text).toBe("not-json{");
  });

  it("ignores 'pane' payloads that decode to a non-string", async () => {
    const { result } = renderHook(() => usePaneStream("alpha", true));
    await waitFor(() => expect(subscribed.length).toBe(1));

    await act(async () => {
      lastSub().opts.onmessage?.({
        id: "3",
        event: "pane",
        data: JSON.stringify({ unexpected: "object" }),
      });
    });
    // No update — text stays at the initial empty string.
    expect(result.current.text).toBe("");
  });

  it("flips ended=true on a 'pane_end' event", async () => {
    const { result } = renderHook(() => usePaneStream("alpha", true));
    await waitFor(() => expect(subscribed.length).toBe(1));

    await act(async () => {
      lastSub().opts.onmessage?.({
        id: "4",
        event: "pane_end",
        data: "",
      });
    });
    expect(result.current.ended).toBe(true);
  });

  it("ignores unknown event types", async () => {
    const { result } = renderHook(() => usePaneStream("alpha", true));
    await waitFor(() => expect(subscribed.length).toBe(1));

    await act(async () => {
      lastSub().opts.onmessage?.({
        id: "5",
        event: "totally_unknown",
        data: JSON.stringify("ignored"),
      });
    });
    expect(result.current.text).toBe("");
    expect(result.current.ended).toBe(false);
  });

  it("transitions connected back to false on a transient onerror", async () => {
    const { result } = renderHook(() => usePaneStream("alpha", true));
    await waitFor(() => expect(subscribed.length).toBe(1));

    // Open first so connected flips to true.
    await act(async () => {
      await lastSub().opts.onopen?.(
        new Response(null, {
          status: 200,
          headers: { "content-type": "text/event-stream" },
        }),
      );
    });
    expect(result.current.connected).toBe(true);

    // Transient error — the hook should clear connected but not throw,
    // letting fetch-event-source retry.
    await act(async () => {
      lastSub().opts.onerror?.(new Error("transient"));
    });
    expect(result.current.connected).toBe(false);
  });

  it("re-throws UnauthorizedError from onerror so retry loop stops", async () => {
    renderHook(() => usePaneStream("alpha", true));
    await waitFor(() => expect(subscribed.length).toBe(1));

    expect(() => {
      lastSub().opts.onerror?.(new UnauthorizedError("401"));
    }).toThrow(UnauthorizedError);
  });

  it("aborts the underlying fetch on unmount", async () => {
    const { unmount } = renderHook(() => usePaneStream("alpha", true));
    await waitFor(() => expect(subscribed.length).toBe(1));
    const sub = lastSub();
    expect(sub.aborted).toBe(false);

    unmount();
    expect(sub.aborted).toBe(true);
  });

  it("aborts and re-subscribes when sessionName changes", async () => {
    const { rerender } = renderHook(
      ({ name }: { name: string }) => usePaneStream(name, true),
      { initialProps: { name: "alpha" } },
    );
    await waitFor(() => expect(subscribed.length).toBe(1));
    const first = subscribed[0];
    expect(first.url).toBe("/events/session/alpha/pane");

    rerender({ name: "beta" });
    await waitFor(() => expect(subscribed.length).toBe(2));
    expect(first.aborted).toBe(true);
    expect(subscribed[1].url).toBe("/events/session/beta/pane");
  });

  it("aborts when toggled from enabled=true to enabled=false", async () => {
    const { rerender } = renderHook(
      ({ enabled }: { enabled: boolean }) =>
        usePaneStream("alpha", enabled),
      { initialProps: { enabled: true } },
    );
    await waitFor(() => expect(subscribed.length).toBe(1));
    const sub = lastSub();
    expect(sub.aborted).toBe(false);

    rerender({ enabled: false });
    expect(sub.aborted).toBe(true);
    // No new subscription opened.
    expect(subscribed.length).toBe(1);
  });

  it("resets connected and ended when re-subscribing", async () => {
    const { result, rerender } = renderHook(
      ({ name }: { name: string }) => usePaneStream(name, true),
      { initialProps: { name: "alpha" } },
    );
    await waitFor(() => expect(subscribed.length).toBe(1));

    // Drive the alpha subscription into a connected + ended state.
    await act(async () => {
      await lastSub().opts.onopen?.(
        new Response(null, {
          status: 200,
          headers: { "content-type": "text/event-stream" },
        }),
      );
      lastSub().opts.onmessage?.({ id: "1", event: "pane_end", data: "" });
    });
    expect(result.current.connected).toBe(true);
    expect(result.current.ended).toBe(true);

    // Switching the name resets local state.
    rerender({ name: "beta" });
    await waitFor(() => expect(subscribed.length).toBe(2));
    expect(result.current.connected).toBe(false);
    expect(result.current.ended).toBe(false);
  });
});

/*
 * Skipped (jsdom limitations): the real fetch-event-source library's
 * automatic reconnect/backoff loop, the visibility-change reopen path,
 * and any actual SSE wire framing. The library is mocked above; the
 * hook's contract is exercised through the mock's callbacks.
 */
