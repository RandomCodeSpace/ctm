import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { MemoryRouter } from "react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { SearchPalette } from "@/components/SearchPalette";
import { useHotkey } from "@/hooks/useHotkey";
import type { SearchResponse } from "@/hooks/useSearch";

/** Mock react-router's useNavigate so we can assert navigation without
 *  depending on JSDOM's history implementation. */
const navigateMock = vi.fn();
vi.mock("react-router", async () => {
  const actual =
    await vi.importActual<typeof import("react-router")>("react-router");
  return {
    ...actual,
    useNavigate: () => navigateMock,
  };
});

function makeResponse(partial: Partial<SearchResponse>): SearchResponse {
  return {
    query: partial.query ?? "needle",
    matches: partial.matches ?? [],
    scanned_files: partial.scanned_files ?? 1,
    truncated: partial.truncated ?? false,
  };
}

function setupFetch(resp: SearchResponse) {
  const fetchMock = vi.fn(
    async (input: RequestInfo | URL): Promise<Response> => {
      void input;
      return new Response(JSON.stringify(resp), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    },
  );
  globalThis.fetch = fetchMock as unknown as typeof globalThis.fetch;
  return fetchMock;
}

function callUrl(call: [RequestInfo | URL, ...unknown[]]): string {
  const first = call[0];
  return typeof first === "string" ? first : first.toString();
}

/** Host component wiring a Cmd+K hotkey to the palette — mirrors
 *  Dashboard's integration so open-on-hotkey is actually exercised. */
function HotkeyHost() {
  const [open, setOpen] = useState(false);
  useHotkey(["mod+k"], () => setOpen(true));
  return <SearchPalette open={open} onClose={() => setOpen(false)} />;
}

function renderPalette(open: boolean, sessionName?: string) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const onClose = vi.fn();
  const utils = render(
    <MemoryRouter>
      <QueryClientProvider client={qc}>
        <SearchPalette
          open={open}
          onClose={onClose}
          sessionName={sessionName}
        />
      </QueryClientProvider>
    </MemoryRouter>,
  );
  return { ...utils, onClose, qc };
}

describe("SearchPalette", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    navigateMock.mockReset();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it("opens on Cmd+K and closes on Esc", async () => {
    setupFetch(makeResponse({ matches: [] }));
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    render(
      <MemoryRouter>
        <QueryClientProvider client={qc}>
          <HotkeyHost />
        </QueryClientProvider>
      </MemoryRouter>,
    );

    const user = userEvent.setup();

    // Palette closed initially — combobox input absent.
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();

    await user.keyboard("{Meta>}k{/Meta}");

    const input = await screen.findByRole("combobox");
    expect(input).toBeInTheDocument();

    // Esc closes via radix's built-in handler.
    await user.keyboard("{Escape}");
    await waitFor(() =>
      expect(screen.queryByRole("combobox")).not.toBeInTheDocument(),
    );
  });

  it("debounces — a rapid keystroke burst only triggers the final query", async () => {
    const fetchMock = setupFetch(makeResponse({ matches: [] }));
    renderPalette(true);

    const input = screen.getByRole("combobox") as HTMLInputElement;

    // Fire rapid intermediate change events with no awaits between them,
    // mirroring a real-world burst. Real timers handle the 200ms debounce.
    // fireEvent.change() routes through React's synthetic event system,
    // unlike a raw .value = … assignment which React ignores.
    await act(async () => {
      input.focus();
      for (const v of ["n", "ne", "nee", "need", "needl", "needle"]) {
        fireEvent.change(input, { target: { value: v } });
      }
    });

    // Wait until the settled query fires. Because each keystroke resets
    // the 200ms timer, only the final "needle" should ever be committed
    // — intermediate prefixes never reach the network.
    await waitFor(
      () => {
        expect(fetchMock).toHaveBeenCalled();
      },
      { timeout: 1000 },
    );

    // All calls should be for the final term; no fetch for "n", "ne", etc.
    for (const call of fetchMock.mock.calls) {
      expect(callUrl(call)).toContain("q=needle");
    }
    // Exactly one fetch — the burst collapsed into a single request.
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("renders matches with a highlighted span around the query", async () => {
    setupFetch(
      makeResponse({
        matches: [
          {
            session: "alpha",
            uuid: "u-1",
            tool: "Bash",
            ts: new Date().toISOString(),
            snippet: "find the needle in the haystack",
          },
        ],
      }),
    );
    renderPalette(true);

    const input = screen.getByRole("combobox");
    await userEvent.type(input, "needle");

    // Wait out debounce + fetch.
    const hits = await screen.findAllByTestId("search-hit");
    expect(hits.length).toBeGreaterThan(0);
    expect(hits[0]?.textContent?.toLowerCase()).toBe("needle");
  });

  it('shows "No matches" when the response is empty', async () => {
    setupFetch(makeResponse({ matches: [], truncated: false }));
    renderPalette(true);

    const input = screen.getByRole("combobox");
    await userEvent.type(input, "zzz");

    expect(await screen.findByText(/no matches/i)).toBeInTheDocument();
  });

  it("shows the truncation notice when truncated=true", async () => {
    setupFetch(
      makeResponse({
        matches: Array.from({ length: 3 }).map((_, i) => ({
          session: "alpha",
          uuid: `u-${i}`,
          snippet: `hit ${i} needle`,
        })),
        truncated: true,
      }),
    );
    renderPalette(true);

    const input = screen.getByRole("combobox");
    await userEvent.type(input, "needle");

    expect(
      await screen.findByText(/refine query/i),
    ).toBeInTheDocument();
  });

  it("navigates to /s/:session when a row is clicked", async () => {
    setupFetch(
      makeResponse({
        matches: [
          {
            session: "alpha",
            uuid: "u-1",
            tool: "Bash",
            snippet: "first needle",
          },
        ],
      }),
    );
    renderPalette(true);

    const input = screen.getByRole("combobox");
    await userEvent.type(input, "needle");

    const row = await screen.findByRole("option");
    await userEvent.click(row.querySelector("button")!);

    expect(navigateMock).toHaveBeenCalledWith("/s/alpha");
  });

  it("prefixes the session filter when sessionName is provided", async () => {
    const fetchMock = setupFetch(makeResponse({ matches: [] }));
    renderPalette(true, "alpha");

    const input = screen.getByRole("combobox");
    await userEvent.type(input, "needle");

    await waitFor(() => expect(fetchMock).toHaveBeenCalled());
    expect(callUrl(fetchMock.mock.calls[0])).toContain("session=alpha");
  });
});

