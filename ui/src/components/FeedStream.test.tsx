import { describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { FeedStream } from "@/components/FeedStream";
import type { ToolCallRow } from "@/hooks/useFeed";
import type { FeedHistoryResponse } from "@/hooks/useFeedHistory";

/** Render FeedStream with a pre-seeded feed cache for sessionName. */
function renderWithCache(
  ui: React.ReactNode,
  {
    sessionName,
    feed,
  }: { sessionName: string; feed: ToolCallRow[] },
) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  qc.setQueryData(["feed", sessionName], feed);
  return render(<QueryClientProvider client={qc}>{ui}</QueryClientProvider>);
}

const liveRow: ToolCallRow = {
  session: "alpha",
  tool: "Bash",
  input: "live command",
  summary: "ok",
  is_error: false,
  ts: "2026-04-21T16:30:00Z",
};

function makeHistoryResponse(
  count: number,
  has_more: boolean,
): FeedHistoryResponse {
  const events = Array.from({ length: count }).map((_, i) => ({
    id: `${i}-0`,
    session: "alpha",
    type: "tool_call",
    ts: `2026-04-21T16:00:0${i}Z`,
    payload: {
      session: "alpha",
      tool: "Edit",
      input: `historical-file-${i}.ts`,
      summary: "diff applied",
      is_error: false,
      ts: `2026-04-21T16:00:0${i}Z`,
    } as ToolCallRow,
  }));
  return { events, has_more };
}

describe("FeedStream — Load older (V6)", () => {
  it("appends returned rows below the ring view on click", async () => {
    const onLoadOlder = vi.fn<
      (beforeId: string) => Promise<FeedHistoryResponse>
    >(async () => makeHistoryResponse(3, true));

    renderWithCache(
      <FeedStream sessionName="alpha" onLoadOlder={onLoadOlder} />,
      { sessionName: "alpha", feed: [liveRow] },
    );

    // Live row is visible.
    expect(screen.getByText("live command")).toBeInTheDocument();

    const button = screen.getByRole("button", { name: /load older/i });
    await userEvent.click(button);

    // Fetcher received the live row's cursor (nanos of 2026-04-21T16:30Z,
    // any suffix "-0"). We only verify it was called with a non-empty
    // string, not the exact ms precision.
    expect(onLoadOlder).toHaveBeenCalledTimes(1);
    expect(onLoadOlder.mock.calls.length).toBe(1);
    const cursor = onLoadOlder.mock.calls[0]![0];
    expect(cursor).toMatch(/^\d+-\d+$/);

    // All three historical rows rendered (newest-first below live).
    await waitFor(() => {
      expect(screen.getByText("historical-file-0.ts")).toBeInTheDocument();
    });
    expect(screen.getByText("historical-file-1.ts")).toBeInTheDocument();
    expect(screen.getByText("historical-file-2.ts")).toBeInTheDocument();

    // has_more=true → button still rendered for another click.
    expect(
      screen.getByRole("button", { name: /load older/i }),
    ).toBeInTheDocument();
  });

  it("hides the Load older button when has_more is false", async () => {
    const onLoadOlder = vi.fn<
      (beforeId: string) => Promise<FeedHistoryResponse>
    >(async () => makeHistoryResponse(2, false));

    renderWithCache(
      <FeedStream sessionName="alpha" onLoadOlder={onLoadOlder} />,
      { sessionName: "alpha", feed: [liveRow] },
    );

    await userEvent.click(screen.getByRole("button", { name: /load older/i }));

    // Rows landed…
    await waitFor(() => {
      expect(screen.getByText("historical-file-0.ts")).toBeInTheDocument();
    });
    // …and the button is gone because the backend reported exhaustion.
    expect(
      screen.queryByRole("button", { name: /load older/i }),
    ).not.toBeInTheDocument();
  });

  it("omits the Load older button entirely when onLoadOlder is absent", () => {
    renderWithCache(<FeedStream sessionName="alpha" />, {
      sessionName: "alpha",
      feed: [liveRow],
    });
    expect(
      screen.queryByRole("button", { name: /load older/i }),
    ).not.toBeInTheDocument();
  });
});
