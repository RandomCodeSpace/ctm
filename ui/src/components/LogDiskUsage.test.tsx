import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { LogDiskUsage } from "@/components/LogDiskUsage";
import { humanBytes } from "@/lib/format";
import type { LogsUsage } from "@/hooks/useLogsUsage";

function renderWithQuery(ui: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

function mockResponse(body: LogsUsage, status = 200) {
  return vi.fn(async () =>
    new Response(JSON.stringify(body), {
      status,
      headers: { "content-type": "application/json" },
    }),
  );
}

describe("humanBytes", () => {
  it("renders B / KB / MB / GB with binary unit boundaries", () => {
    expect(humanBytes(0)).toBe("0 B");
    expect(humanBytes(1023)).toBe("1023 B");
    expect(humanBytes(1024)).toBe("1 KB");
    expect(humanBytes(1024 * 1024)).toBe("1 MB");
    expect(humanBytes(1024 * 1024 * 1024)).toBe("1 GB");
    // Non-integer scaled value keeps one decimal.
    expect(humanBytes(1024 + 512)).toBe("1.5 KB");
    expect(humanBytes(1024 * 1024 + 512 * 1024)).toBe("1.5 MB");
  });

  it("handles invalid input without throwing", () => {
    expect(humanBytes(NaN)).toBe("—");
    expect(humanBytes(Infinity)).toBe("—");
  });
});

describe("LogDiskUsage", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("renders total bytes and per-session rows from the hook", async () => {
    const payload: LogsUsage = {
      dir: "/home/dev/.config/ctm/logs",
      total_bytes: 1_048_576 + 1024, // 1 MB + 1 KB
      files: [
        {
          uuid: "aaaa",
          session: "alpha",
          bytes: 1_048_576,
          mtime: new Date(Date.now() - 120_000).toISOString(),
        },
        {
          uuid: "bbbb",
          session: "beta",
          bytes: 1024,
          mtime: new Date(Date.now() - 300_000).toISOString(),
        },
      ],
    };
    globalThis.fetch = mockResponse(
      payload,
    ) as unknown as typeof globalThis.fetch;

    renderWithQuery(<LogDiskUsage />);

    // Total is rendered in the header.
    await waitFor(() => {
      const region = screen.getByRole("region", { name: /log disk usage/i });
      expect(within(region).getByText("1 MB", { exact: false })).toBeTruthy();
    });

    const region = screen.getByRole("region", { name: /log disk usage/i });
    // Both session names rendered.
    expect(within(region).getByText("alpha")).toBeInTheDocument();
    expect(within(region).getByText("beta")).toBeInTheDocument();
    // Per-row sizes humanised. "1 MB" is in the header too, so assert
    // on beta's 1 KB row which is unique to the body.
    expect(within(region).getByText("1 KB")).toBeInTheDocument();
    // Dir surfaced in the footer.
    expect(
      within(region).getByText("/home/dev/.config/ctm/logs"),
    ).toBeInTheDocument();
  });

  it("renders the empty state when no log files exist", async () => {
    globalThis.fetch = mockResponse({
      dir: "/fresh/install",
      total_bytes: 0,
      files: [],
    }) as unknown as typeof globalThis.fetch;

    renderWithQuery(<LogDiskUsage />);

    await waitFor(() => {
      expect(
        screen.getByText(/no log files in \/fresh\/install/i),
      ).toBeInTheDocument();
    });
  });

  it("surfaces an error when the endpoint fails", async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response("boom", { status: 500 }),
    ) as unknown as typeof globalThis.fetch;

    renderWithQuery(<LogDiskUsage />);

    await waitFor(() => {
      expect(
        screen.getByRole("alert", { name: undefined }),
      ).toHaveTextContent(/could not load log usage/i);
    });
  });
});
