import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { DiffSheet, classifyLine } from "@/components/DiffSheet";
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

function renderWithQuery(ui: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={client}>{ui}</QueryClientProvider>,
  );
}

// Representative diff snippet covering every branch of classifyLine.
const SAMPLE_DIFF = [
  "commit abcdef1234567890abcdef1234567890abcdef12",
  "Author: ctm",
  "",
  "diff --git a/foo.go b/foo.go",
  "index 111..222 100644",
  "--- a/foo.go",
  "+++ b/foo.go",
  "@@ -1,3 +1,3 @@",
  " context line",
  "-removed line",
  "+added line",
].join("\n");

describe("DiffSheet", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it("classifies +/- /@@/plain lines into the expected colour buckets", () => {
    expect(classifyLine("+added")).toBe("text-emerald-400");
    expect(classifyLine("-removed")).toBe("text-alert-ember");
    expect(classifyLine("@@ -1,3 +1,3 @@")).toBe("text-fg-dim");
    expect(classifyLine(" context")).toBe("text-fg");
    expect(classifyLine("commit abc")).toBe("text-fg");
  });

  it("fetches the diff and renders every line with the right colour", async () => {
    const fetchMock = vi.fn(async (url: RequestInfo | URL) => {
      expect(String(url)).toContain(
        `/api/sessions/alpha/checkpoints/${FULL_SHA}/diff`,
      );
      return new Response(SAMPLE_DIFF, {
        status: 200,
        headers: { "content-type": "text/plain" },
      });
    });
    globalThis.fetch = fetchMock as unknown as typeof globalThis.fetch;

    renderWithQuery(
      <DiffSheet
        sessionName="alpha"
        checkpoint={makeCheckpoint()}
        onClose={() => {}}
      />,
    );

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled();
    });

    const pre = await screen.findByTestId("diff-pre");
    expect(pre).toBeInTheDocument();

    // Added line gets emerald.
    await waitFor(() => {
      const added = screen.getByText("+added line");
      expect(added.className).toContain("text-emerald-400");
    });
    // Removed line gets alert-ember.
    const removed = screen.getByText("-removed line");
    expect(removed.className).toContain("text-alert-ember");
    // Hunk header gets fg-dim.
    const hunk = screen.getByText("@@ -1,3 +1,3 @@");
    expect(hunk.className).toContain("text-fg-dim");
    // Plain commit-header line stays on default fg.
    const commit = screen.getByText(
      "commit abcdef1234567890abcdef1234567890abcdef12",
    );
    expect(commit.className).toContain("text-fg");
    expect(commit.className).not.toContain("text-emerald-400");
    expect(commit.className).not.toContain("text-alert-ember");
  });

  it("does not fetch when no checkpoint is provided", () => {
    const fetchMock = vi.fn();
    globalThis.fetch = fetchMock as unknown as typeof globalThis.fetch;
    renderWithQuery(
      <DiffSheet
        sessionName="alpha"
        checkpoint={null}
        onClose={() => {}}
      />,
    );
    expect(fetchMock).not.toHaveBeenCalled();
    expect(screen.queryByText(/checkpoint diff/i)).toBeNull();
  });

  it("surfaces a fetch error in an alert region", async () => {
    const fetchMock = vi.fn(async () => {
      return new Response("boom", {
        status: 500,
        headers: { "content-type": "text/plain" },
      });
    });
    globalThis.fetch = fetchMock as unknown as typeof globalThis.fetch;

    renderWithQuery(
      <DiffSheet
        sessionName="alpha"
        checkpoint={makeCheckpoint()}
        onClose={() => {}}
      />,
    );

    await waitFor(() => {
      expect(screen.getByRole("alert")).toBeInTheDocument();
    });
    expect(screen.getByRole("alert")).toHaveTextContent(/could not load diff/i);
  });
});
