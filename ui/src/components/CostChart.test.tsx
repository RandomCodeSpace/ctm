import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { CostChart } from "@/components/CostChart";
import type { CostResponse } from "@/hooks/useCost";

function renderWithQuery(ui: React.ReactNode) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(<QueryClientProvider client={client}>{ui}</QueryClientProvider>);
}

function makeResponse(window: "hour" | "day" | "week" = "day"): CostResponse {
  const now = Date.now();
  return {
    window,
    points: [
      {
        ts: new Date(now - 10 * 60_000).toISOString(),
        session: "alpha",
        input_tokens: 1000,
        output_tokens: 500,
        cache_tokens: 100,
        cost_usd_micros: 12_000,
      },
      {
        ts: new Date(now - 5 * 60_000).toISOString(),
        session: "alpha",
        input_tokens: 2000,
        output_tokens: 1000,
        cache_tokens: 200,
        cost_usd_micros: 24_000,
      },
    ],
    totals: {
      input: 2000,
      output: 1000,
      cache: 200,
      cost_usd_micros: 24_000,
    },
  };
}

describe("CostChart", () => {
  let originalFetch: typeof globalThis.fetch;

  beforeEach(() => {
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    vi.restoreAllMocks();
  });

  it("renders a skeleton while loading", async () => {
    // Pending forever — exercises the loading path.
    globalThis.fetch = vi.fn(
      () => new Promise(() => {}),
    ) as unknown as typeof globalThis.fetch;

    const { container } = renderWithQuery(<CostChart />);
    // design-system Skeleton renders with class `rcs-skeleton`.
    await waitFor(() => {
      expect(container.querySelector(".rcs-skeleton")).toBeTruthy();
    });
  });

  it("renders a polyline when data is present", async () => {
    globalThis.fetch = vi.fn(
      async () =>
        new Response(JSON.stringify(makeResponse()), {
          status: 200,
          headers: { "content-type": "application/json" },
        }),
    ) as unknown as typeof globalThis.fetch;

    renderWithQuery(<CostChart />);

    const region = await screen.findByRole("region", {
      name: /cumulative cost/i,
    });
    // Polyline is present and has a non-empty points attribute.
    const poly = await waitFor(() => {
      const node = region.querySelector(
        '[data-testid="cost-polyline"]',
      ) as SVGPolylineElement | null;
      expect(node).not.toBeNull();
      return node!;
    });
    expect(poly.getAttribute("points")).toBeTruthy();
    expect(poly.getAttribute("points")!.length).toBeGreaterThan(0);

    // Totals row renders.
    expect(within(region).getByText(/\$0\.0240/)).toBeInTheDocument();
    expect(within(region).getByText(/3k tokens/i)).toBeInTheDocument();
    // Cache ratio: cache=200, input=2000 → 200/2200 ≈ 9%.
    expect(within(region).getByText(/cache hit 9%/i)).toBeInTheDocument();
  });

  it("renders empty state when points=[]", async () => {
    globalThis.fetch = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            window: "day",
            points: [],
            totals: { input: 0, output: 0, cache: 0, cost_usd_micros: 0 },
          }),
          { status: 200, headers: { "content-type": "application/json" } },
        ),
    ) as unknown as typeof globalThis.fetch;

    renderWithQuery(<CostChart />);

    await waitFor(() => {
      expect(
        screen.getByText(/no cost data yet/i),
      ).toBeInTheDocument();
    });
  });

  it("window pill click re-fires fetch with the new window", async () => {
    const fetchMock = vi.fn(async (url: RequestInfo | URL) => {
      const href = typeof url === "string" ? url : url.toString();
      const win = new URL(href, "http://x").searchParams.get("window") ?? "day";
      return new Response(JSON.stringify(makeResponse(win as "hour" | "day" | "week")), {
        status: 200,
        headers: { "content-type": "application/json" },
      });
    });
    globalThis.fetch = fetchMock as unknown as typeof globalThis.fetch;

    renderWithQuery(<CostChart />);

    // Wait for initial load (window=day).
    await screen.findByRole("region", { name: /cumulative cost/i });
    await waitFor(() => {
      expect(fetchMock).toHaveBeenCalled();
    });
    const initialCalls = fetchMock.mock.calls.length;
    const user = userEvent.setup();
    await user.click(screen.getByRole("tab", { name: /hour/i }));

    await waitFor(() => {
      expect(fetchMock.mock.calls.length).toBeGreaterThan(initialCalls);
    });
    const lastCall = fetchMock.mock.calls.at(-1)!;
    const lastUrl =
      typeof lastCall[0] === "string" ? lastCall[0] : String(lastCall[0]);
    expect(lastUrl).toContain("window=hour");
  });
});
