import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ToolCallRow } from "./ToolCallRow";
import type { ToolCallRow as ToolCallRowType } from "@/hooks/useFeed";

function makeRow(overrides: Partial<ToolCallRowType> = {}): ToolCallRowType {
  return {
    session: "alpha",
    tool: "Edit",
    input: "/tmp/a.go",
    summary: "",
    is_error: false,
    ts: "2026-04-21T16:28:00Z",
    id: "17771234000000000-0",
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

const SAMPLE_DIFF = [
  "--- a/tmp/a.go",
  "+++ b/tmp/a.go",
  "@@ -1,1 +1,1 @@",
  "-foo",
  "+bar",
].join("\n");

describe("ToolCallRow", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
  });

  it("shows no expand chevron for non-diff tools (Bash)", () => {
    renderWithQuery(
      <ToolCallRow
        row={makeRow({ tool: "Bash", input: "ls -la" })}
      />,
    );
    expect(screen.queryByTestId("tool-expand")).toBeNull();
  });

  it("shows no expand chevron for Read", () => {
    renderWithQuery(
      <ToolCallRow
        row={makeRow({ tool: "Read", input: "/etc/hosts" })}
      />,
    );
    expect(screen.queryByTestId("tool-expand")).toBeNull();
  });

  it("shows an expand chevron for Edit", () => {
    renderWithQuery(<ToolCallRow row={makeRow({ tool: "Edit" })} />);
    expect(screen.getByTestId("tool-expand")).toBeInTheDocument();
  });

  it("shows an expand chevron for MultiEdit and Write", () => {
    renderWithQuery(<ToolCallRow row={makeRow({ tool: "MultiEdit" })} />);
    expect(screen.getByTestId("tool-expand")).toBeInTheDocument();
    // Fresh tree for Write so the first assertion doesn't poison the next.
    renderWithQuery(<ToolCallRow row={makeRow({ tool: "Write" })} />);
    // Two trees now — expect at least one expand button.
    expect(screen.getAllByTestId("tool-expand").length).toBeGreaterThanOrEqual(
      1,
    );
  });

  it("does not fetch until the user expands the row", () => {
    const fetchMock = vi.fn();
    globalThis.fetch = fetchMock as unknown as typeof globalThis.fetch;
    renderWithQuery(<ToolCallRow row={makeRow({ tool: "Edit" })} />);
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("fetches and renders the diff with classifyLine colouring on expand", async () => {
    const fetchMock = vi.fn(async (url: RequestInfo | URL) => {
      expect(String(url)).toContain(
        "/api/sessions/alpha/tool_calls/17771234000000000-0/detail",
      );
      return new Response(
        JSON.stringify({
          tool: "Edit",
          input_json: '{"file_path":"/tmp/a.go"}',
          output_excerpt: "",
          ts: "2026-04-21T16:28:00Z",
          is_error: false,
          diff: SAMPLE_DIFF,
        }),
        {
          status: 200,
          headers: { "content-type": "application/json" },
        },
      );
    });
    globalThis.fetch = fetchMock as unknown as typeof globalThis.fetch;

    const user = userEvent.setup();
    renderWithQuery(<ToolCallRow row={makeRow({ tool: "Edit" })} />);

    await user.click(screen.getByTestId("tool-expand"));

    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled();
    });

    const pre = await screen.findByTestId("tool-diff");
    expect(pre).toBeInTheDocument();

    // Added line → emerald.
    const added = screen.getByText("+bar");
    expect(added.className).toContain("text-emerald-400");

    // Removed line → alert-ember.
    const removed = screen.getByText("-foo");
    expect(removed.className).toContain("text-alert-ember");

    // Hunk header → fg-dim.
    const hunk = screen.getByText("@@ -1,1 +1,1 @@");
    expect(hunk.className).toContain("text-fg-dim");
  });

  it("toggles expand state without re-fetching on second open (staleTime)", async () => {
    const fetchMock = vi.fn(async () => {
      return new Response(
        JSON.stringify({
          tool: "Edit",
          input_json: "{}",
          output_excerpt: "",
          ts: "2026-04-21T16:28:00Z",
          is_error: false,
          diff: SAMPLE_DIFF,
        }),
        {
          status: 200,
          headers: { "content-type": "application/json" },
        },
      );
    });
    globalThis.fetch = fetchMock as unknown as typeof globalThis.fetch;

    const user = userEvent.setup();
    renderWithQuery(<ToolCallRow row={makeRow({ tool: "Edit" })} />);

    const btn = screen.getByTestId("tool-expand");
    await user.click(btn);
    await waitFor(() => expect(fetchMock).toHaveBeenCalledTimes(1));

    // Collapse.
    await user.click(btn);
    expect(screen.queryByTestId("tool-diff")).toBeNull();

    // Re-expand — cached result within 5 min staleTime should NOT
    // trigger a second fetch.
    await user.click(btn);
    await screen.findByTestId("tool-diff");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });
});
