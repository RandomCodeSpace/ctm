import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { RevertSheet } from "@/components/RevertSheet";
import type { Checkpoint } from "@/hooks/useCheckpoints";

const FULL_SHA = "abcdef1234567890abcdef1234567890abcdef12";

function makeCheckpoint(overrides: Partial<Checkpoint> = {}): Checkpoint {
  return {
    sha: FULL_SHA,
    short_sha: FULL_SHA.slice(0, 7),
    subject: "checkpoint: pre-yolo 2026-04-20T12:00:00",
    author: "ctm",
    ts: new Date(Date.now() - 10_000).toISOString(),
    ...overrides,
  };
}

describe("RevertSheet", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.useRealTimers();
  });

  it("sends the FULL sha (not the short sha) on first revert", async () => {
    const fetchMock = vi.fn(async (_url: RequestInfo | URL, init?: RequestInit) => {
      // Inspect the body the component sent.
      const body = JSON.parse(String(init?.body ?? "{}"));
      expect(body.sha).toBe(FULL_SHA);
      expect(body.stash_first).toBe(false);
      return new Response(
        JSON.stringify({ ok: true, reverted_to: FULL_SHA }),
        { status: 200, headers: { "content-type": "application/json" } },
      );
    });
    globalThis.fetch = fetchMock as unknown as typeof globalThis.fetch;

    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(
      <RevertSheet
        sessionName="claude"
        checkpoint={makeCheckpoint()}
        onClose={() => {}}
      />,
    );

    await user.click(screen.getByRole("button", { name: /^revert$/i }));
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalledTimes(1);
    });
  });

  it("blocks revert on 409 dirty workdir until stash is chosen", async () => {
    const calls: Array<{ stashFirst: boolean }> = [];
    const fetchMock = vi.fn(async (_url, init?: RequestInit) => {
      const body = JSON.parse(String(init?.body ?? "{}"));
      calls.push({ stashFirst: body.stash_first });
      if (!body.stash_first) {
        return new Response(
          JSON.stringify({
            error: "dirty_workdir",
            dirty_files: ["a.go", "b.txt"],
          }),
          { status: 409, headers: { "content-type": "application/json" } },
        );
      }
      return new Response(
        JSON.stringify({
          ok: true,
          reverted_to: FULL_SHA,
          stashed_as: "stash@{0}",
        }),
        { status: 200, headers: { "content-type": "application/json" } },
      );
    });
    globalThis.fetch = fetchMock as unknown as typeof globalThis.fetch;

    const onClose = vi.fn();
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    render(
      <RevertSheet
        sessionName="claude"
        checkpoint={makeCheckpoint()}
        onClose={onClose}
      />,
    );

    // First revert returns 409 dirty.
    await user.click(screen.getByRole("button", { name: /^revert$/i }));

    await waitFor(() => {
      expect(screen.getByText(/working tree dirty/i)).toBeInTheDocument();
    });
    expect(screen.getByText("a.go")).toBeInTheDocument();
    expect(screen.getByText("b.txt")).toBeInTheDocument();

    // The plain "Revert" button is gone — only "Stash first then revert".
    expect(screen.queryByRole("button", { name: /^revert$/i })).toBeNull();
    const stashBtn = screen.getByRole("button", {
      name: /stash first then revert/i,
    });

    await user.click(stashBtn);

    await waitFor(() => {
      expect(calls.length).toBe(2);
    });
    expect(calls[0]).toEqual({ stashFirst: false });
    expect(calls[1]).toEqual({ stashFirst: true });

    // Success state shows the new HEAD + stash ref.
    await waitFor(() => {
      expect(screen.getByText(/stash@\{0\}/)).toBeInTheDocument();
    });
  });

  it("renders nothing when no checkpoint is provided", () => {
    render(
      <RevertSheet
        sessionName="claude"
        checkpoint={null}
        onClose={() => {}}
      />,
    );
    expect(screen.queryByText(/revert to checkpoint/i)).toBeNull();
  });
});
