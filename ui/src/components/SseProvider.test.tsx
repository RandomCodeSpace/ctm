import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, waitFor, act } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { AuthProvider } from "@/components/AuthProvider";
import { SseProvider } from "@/components/SseProvider";
import { TOKEN_KEY } from "@/lib/api";

/*
 * Mock fetch-event-source so we can count subscribe/unsubscribe lifecycles.
 * Each call to fetchEventSource returns a new "session"; we record open
 * and abort.
 */
const subscribed: Array<{ url: string; aborted: boolean }> = [];

vi.mock("@microsoft/fetch-event-source", () => {
  return {
    fetchEventSource: vi.fn(
      async (
        url: string,
        opts: { signal?: AbortSignal; onopen?: (r: Response) => void },
      ) => {
        const entry = { url, aborted: false };
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
      },
    ),
  };
});

function Tree() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return (
    <QueryClientProvider client={qc}>
      <AuthProvider>
        <SseProvider>
          <div>app</div>
        </SseProvider>
      </AuthProvider>
    </QueryClientProvider>
  );
}

describe("SseProvider", () => {
  beforeEach(() => {
    subscribed.length = 0;
    localStorage.clear();
  });

  afterEach(() => {
    localStorage.clear();
  });

  it("subscribes once when a token is present and aborts on token change", async () => {
    localStorage.setItem(TOKEN_KEY, "token-1");
    const { unmount } = render(<Tree />);

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
      // The previous subscription must have aborted (signal triggered).
      expect(subscribed[0].aborted).toBe(true);
      // And a new subscription opened — clean re-mount on token change.
      expect(subscribed.length).toBeGreaterThanOrEqual(2);
    });

    unmount();
  });

  it("does not subscribe when no token is present (TokenPasteScreen path)", async () => {
    // No token in storage → AuthProvider renders <TokenPasteScreen>, the
    // SseProvider tree is never mounted under it.
    render(<Tree />);
    await new Promise((r) => setTimeout(r, 30));
    expect(subscribed.length).toBe(0);
  });
});
