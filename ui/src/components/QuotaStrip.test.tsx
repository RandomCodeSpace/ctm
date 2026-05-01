import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QuotaStrip } from "@/components/QuotaStrip";
import type { Quota } from "@/hooks/useQuota";

/*
 * QuotaStrip is a pure presentational component. It reads from
 * `useQuota` (a tanstack query hook) and renders two QuotaBars: 5h +
 * Weekly. We mock useQuota directly — no QueryClient needed — and
 * exercise every quota state:
 *
 *   - undefined data -> "—" placeholders, no progressbar fill
 *   - low (<75%)  -> bg-fg-muted
 *   - mid (>=75%) -> bg-accent-gold
 *   - high (>=90%) -> bg-alert-ember
 *   - over (>100%) -> clamped to 100% on the bar but rounded display value
 *   - reset timers render with relativeFuture and a tooltip
 *
 * Tests use a fixed `Date.now()` via vi.useFakeTimers (no setInterval —
 * format helpers compute once on render) so the relative-time strings
 * are deterministic. All `vi.useFakeTimers` ops are scoped per test
 * and restored in afterEach so no flake leaks across the suite.
 */

const mockUseQuota = vi.fn();

vi.mock("@/hooks/useQuota", () => ({
  useQuota: () => mockUseQuota(),
}));

function setQuota(data: Quota | null | undefined) {
  mockUseQuota.mockReturnValue({ data });
}

const NOW = new Date("2026-04-25T12:00:00Z");

describe("QuotaStrip", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(NOW);
  });

  afterEach(() => {
    vi.useRealTimers();
    mockUseQuota.mockReset();
  });

  it("renders both bars with placeholders when quota data is undefined", () => {
    setQuota(undefined);
    render(<QuotaStrip />);

    // Region wrapper.
    const region = screen.getByRole("region", { name: /rate limit usage/i });
    expect(region).toBeInTheDocument();

    // Two progressbars — one per label.
    const bars = screen.getAllByRole("progressbar");
    expect(bars).toHaveLength(2);

    // Labels in correct order.
    expect(screen.getByText("5h")).toBeInTheDocument();
    expect(screen.getByText("Weekly")).toBeInTheDocument();

    // No aria-valuenow when pct is unknown.
    bars.forEach((b) => {
      expect(b).not.toHaveAttribute("aria-valuenow");
    });
    // Two "—" placeholders, one per bar.
    expect(screen.getAllByText("—")).toHaveLength(2);
  });

  it("renders both bars with placeholders when data is null", () => {
    setQuota(null);
    render(<QuotaStrip />);
    expect(screen.getAllByRole("progressbar")).toHaveLength(2);
    expect(screen.getAllByText("—")).toHaveLength(2);
  });

  it("renders low/nominal usage with the muted fill colour", () => {
    setQuota({
      five_hr_pct: 12,
      weekly_pct: 40,
      five_hr_resets_at: "2026-04-25T16:00:00Z",
      weekly_resets_at: "2026-04-30T00:00:00Z",
    });
    render(<QuotaStrip />);

    const bars = screen.getAllByRole("progressbar");
    expect(bars[0]).toHaveAttribute("aria-valuenow", "12");
    expect(bars[1]).toHaveAttribute("aria-valuenow", "40");

    // Inner fill div — first child of each progressbar.
    const fill5h = bars[0].firstElementChild as HTMLElement;
    const fillWk = bars[1].firstElementChild as HTMLElement;
    expect(fill5h.className).toContain("bg-fg-muted");
    expect(fillWk.className).toContain("bg-fg-muted");
    expect(fill5h.style.width).toBe("12%");
    expect(fillWk.style.width).toBe("40%");

    // Display percentages.
    expect(screen.getByText("12%")).toBeInTheDocument();
    expect(screen.getByText("40%")).toBeInTheDocument();
  });

  it("renders mid usage (>=75%) with the gold warning colour", () => {
    setQuota({
      five_hr_pct: 75,
      weekly_pct: 80,
      five_hr_resets_at: "2026-04-25T16:00:00Z",
      weekly_resets_at: "2026-04-30T00:00:00Z",
    });
    render(<QuotaStrip />);

    const bars = screen.getAllByRole("progressbar");
    const fill5h = bars[0].firstElementChild as HTMLElement;
    const fillWk = bars[1].firstElementChild as HTMLElement;
    expect(fill5h.className).toContain("bg-accent-gold");
    expect(fillWk.className).toContain("bg-accent-gold");
    expect(screen.getByText("75%")).toBeInTheDocument();
    expect(screen.getByText("80%")).toBeInTheDocument();
  });

  it("renders high usage (>=90%) with the ember critical colour", () => {
    setQuota({
      five_hr_pct: 92,
      weekly_pct: 99,
      five_hr_resets_at: "2026-04-25T16:00:00Z",
      weekly_resets_at: "2026-04-30T00:00:00Z",
    });
    render(<QuotaStrip />);

    const bars = screen.getAllByRole("progressbar");
    const fill5h = bars[0].firstElementChild as HTMLElement;
    const fillWk = bars[1].firstElementChild as HTMLElement;
    expect(fill5h.className).toContain("bg-alert-ember");
    expect(fillWk.className).toContain("bg-alert-ember");
    expect(screen.getByText("92%")).toBeInTheDocument();
    expect(screen.getByText("99%")).toBeInTheDocument();
  });

  it("clamps over-100 percentages to a 100%-wide bar but rounds display value", () => {
    setQuota({
      five_hr_pct: 137,
      weekly_pct: -5,
      five_hr_resets_at: "2026-04-25T16:00:00Z",
      weekly_resets_at: "2026-04-30T00:00:00Z",
    });
    render(<QuotaStrip />);

    const bars = screen.getAllByRole("progressbar");
    const fill5h = bars[0].firstElementChild as HTMLElement;
    const fillWk = bars[1].firstElementChild as HTMLElement;

    // Width clamped to 100% / 0%.
    expect(fill5h.style.width).toBe("100%");
    expect(fillWk.style.width).toBe("0%");

    // aria-valuenow reflects the clamped (safe) value, not the raw input.
    expect(bars[0]).toHaveAttribute("aria-valuenow", "100");
    expect(bars[1]).toHaveAttribute("aria-valuenow", "0");

    // Display value comes from the clamped/rounded number.
    expect(screen.getByText("100%")).toBeInTheDocument();
    expect(screen.getByText("0%")).toBeInTheDocument();
  });

  it("rounds fractional percentages for display while keeping width precise", () => {
    setQuota({
      five_hr_pct: 33.6,
      weekly_pct: 50.4,
      five_hr_resets_at: "2026-04-25T16:00:00Z",
      weekly_resets_at: "2026-04-30T00:00:00Z",
    });
    render(<QuotaStrip />);

    expect(screen.getByText("34%")).toBeInTheDocument();
    expect(screen.getByText("50%")).toBeInTheDocument();
    const bars = screen.getAllByRole("progressbar");
    expect((bars[0].firstElementChild as HTMLElement).style.width).toBe(
      "33.6%",
    );
    expect((bars[1].firstElementChild as HTMLElement).style.width).toBe(
      "50.4%",
    );
  });

  it("shows reset-in copy and a tooltip when resetAt is provided", () => {
    setQuota({
      five_hr_pct: 30,
      weekly_pct: 50,
      // 4 hours into the future from the fake NOW.
      five_hr_resets_at: "2026-04-25T16:00:00Z",
      // 5 days into the future.
      weekly_resets_at: "2026-04-30T12:00:00Z",
    });
    render(<QuotaStrip />);

    expect(screen.getByText(/resets in 4 hr/i)).toBeInTheDocument();
    expect(screen.getByText(/resets in 5 days/i)).toBeInTheDocument();

    // Tooltips carry the raw ISO timestamp.
    const fiveHrLabel = screen.getByText(/resets in 4 hr/i);
    expect(fiveHrLabel).toHaveAttribute(
      "title",
      "Resets at 2026-04-25T16:00:00Z",
    );
  });

  it("omits the reset-in copy when resetAt is missing", () => {
    setQuota({
      five_hr_pct: 30,
      weekly_pct: 50,
      // Both reset timestamps blank.
      five_hr_resets_at: "",
      weekly_resets_at: "",
    });
    render(<QuotaStrip />);

    expect(screen.queryByText(/resets in/i)).not.toBeInTheDocument();
  });

  it("uses the bar's aria-label for assistive tech", () => {
    setQuota({
      five_hr_pct: 30,
      weekly_pct: 50,
      five_hr_resets_at: "2026-04-25T16:00:00Z",
      weekly_resets_at: "2026-04-30T12:00:00Z",
    });
    render(<QuotaStrip />);
    expect(
      screen.getByRole("progressbar", { name: /5h quota usage/i }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("progressbar", { name: /weekly quota usage/i }),
    ).toBeInTheDocument();
  });

  it("supports the partial-data case where only one quota track has reset info", () => {
    setQuota({
      five_hr_pct: 22,
      weekly_pct: 88,
      five_hr_resets_at: "2026-04-25T16:00:00Z",
      weekly_resets_at: "",
    });
    render(<QuotaStrip />);

    expect(screen.getByText(/resets in 4 hr/i)).toBeInTheDocument();
    // Only ONE "resets in" line — weekly doesn't render its reset chip.
    expect(screen.getAllByText(/resets in/i)).toHaveLength(1);
  });
});
