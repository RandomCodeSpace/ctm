import { useMemo, useState } from "react";
import { Skeleton } from "@/components/ui/skeleton";
import { useCost, type CostPoint, type CostWindow } from "@/hooks/useCost";
import { cn } from "@/lib/utils";

const WIDTH = 600;
const HEIGHT = 120;
const PADDING = { top: 8, right: 8, bottom: 8, left: 8 };
const INNER_W = WIDTH - PADDING.left - PADDING.right;
const INNER_H = HEIGHT - PADDING.top - PADDING.bottom;

interface Props {
  sessionName?: string;
  className?: string;
}

const WINDOW_LABELS: Record<CostWindow, string> = {
  hour: "Hour",
  day: "Day",
  week: "Week",
};

const WINDOWS: CostWindow[] = ["hour", "day", "week"];

/**
 * V13 cumulative cost chart.
 *
 * Renders a hand-rolled SVG polyline of cumulative USD across the
 * selected window. No d3, no recharts — the line is a <polyline>
 * built from points.reduce(sum, cost_usd_micros) so the bundle stays
 * lean (pattern matches ToolFrequencySparkline).
 *
 * When no session is given, the chart aggregates across every
 * persisted session (daemon-wide cost-over-time).
 */
export function CostChart({ sessionName, className }: Props) {
  const [window, setWindow] = useState<CostWindow>("day");
  const { data, isLoading, isError, error } = useCost(sessionName, window);

  const { polyline, totalUSD, tokenSum, cacheRatio } = useMemo(() => {
    const empty = { polyline: "", totalUSD: 0, tokenSum: 0, cacheRatio: 0 };
    if (!data || data.points.length === 0) return empty;

    const points = data.points;
    // Cumulative running-sum over cost_usd_micros so the line always
    // rises monotonically — reads as "cumulative spend" at a glance.
    const series: { ts: number; cum: number }[] = [];
    let cum = 0;
    for (const p of points) {
      cum += p.cost_usd_micros;
      series.push({ ts: Date.parse(p.ts), cum });
    }
    const firstTs = series[0].ts;
    const lastTs = series[series.length - 1].ts;
    const spanMs = Math.max(1, lastTs - firstTs);
    const peak = series[series.length - 1].cum || 1;

    const pts = series.map((s) => {
      const x = PADDING.left + ((s.ts - firstTs) / spanMs) * INNER_W;
      const y = PADDING.top + INNER_H - (s.cum / peak) * INNER_H;
      return `${x.toFixed(1)},${y.toFixed(1)}`;
    });

    const totals = data.totals;
    const totalUSDval = totals.cost_usd_micros / 1_000_000;
    const tokenSumVal = totals.input + totals.output;
    // Cache hit ratio = cache / (input + cache). 0 when both are zero
    // so the legend never renders NaN.
    const denom = totals.input + totals.cache;
    const cacheRatioVal = denom > 0 ? totals.cache / denom : 0;

    return {
      polyline: pts.join(" "),
      totalUSD: totalUSDval,
      tokenSum: tokenSumVal,
      cacheRatio: cacheRatioVal,
    };
  }, [data]);

  const hasPoints = Boolean(data && data.points.length > 0);
  const ariaLabel = `Cumulative cost over ${WINDOW_LABELS[window].toLowerCase()}, $${totalUSD.toFixed(4)}`;

  return (
    <section
      aria-label="Cumulative cost"
      className={cn(
        "mx-6 my-4 border border-border bg-surface",
        className,
      )}
    >
      <header className="flex items-center justify-between border-b border-border px-4 py-2">
        <h3 className="text-[11px] font-semibold uppercase tracking-[0.18em] text-fg-muted">
          Cumulative cost
        </h3>
        <div role="tablist" aria-label="Cost window" className="flex gap-1">
          {WINDOWS.map((w) => (
            <button
              key={w}
              type="button"
              role="tab"
              aria-selected={window === w}
              onClick={() => setWindow(w)}
              className={cn(
                "rounded-sm px-2.5 py-1 text-[11px] font-semibold uppercase tracking-[0.18em] transition-colors",
                "motion-reduce:transition-none",
                window === w
                  ? "bg-surface-2 text-fg"
                  : "text-fg-muted hover:text-fg",
              )}
            >
              {WINDOW_LABELS[w]}
            </button>
          ))}
        </div>
      </header>

      {isLoading && (
        <div className="space-y-2 p-4">
          <Skeleton className="h-24 w-full" />
        </div>
      )}

      {isError && (
        <p
          role="alert"
          className="m-4 border-l-[3px] border-alert-ember bg-bg px-3 py-2 text-sm text-alert-ember"
        >
          Could not load cost data
          {error instanceof Error ? `: ${error.message}` : ""}
        </p>
      )}

      {!isLoading && !isError && !hasPoints && (
        <p className="px-4 py-8 text-center text-sm text-fg-dim">
          No cost data yet — run a session to start tracking.
        </p>
      )}

      {!isLoading && !isError && hasPoints && (
        <>
          <div className="px-4 py-3">
            <svg
              role="img"
              aria-label={ariaLabel}
              viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
              preserveAspectRatio="none"
              className="block h-[120px] w-full"
            >
              <polyline
                data-testid="cost-polyline"
                fill="none"
                stroke="currentColor"
                strokeWidth={1.5}
                strokeLinejoin="round"
                strokeLinecap="round"
                className={cn(
                  "text-accent",
                  "motion-safe:transition-[stroke-dashoffset] motion-safe:duration-500",
                  "motion-reduce:transition-none",
                )}
                points={polyline}
              />
            </svg>
          </div>
          <footer className="flex flex-wrap items-baseline gap-x-6 gap-y-1 border-t border-border px-4 py-2 text-[11px] text-fg-dim">
            <span
              className="font-mono text-sm tabular-nums text-fg"
              aria-label={`Total cost ${formatUSD(totalUSD)}`}
            >
              {formatUSD(totalUSD)}
            </span>
            <span className="tabular-nums">
              {compactNumber(tokenSum)} tokens
            </span>
            <span className="tabular-nums">
              cache hit {(cacheRatio * 100).toFixed(0)}%
            </span>
          </footer>
        </>
      )}
    </section>
  );
}

/**
 * Format USD with 4 decimals — cumulative cost for a typical dev
 * session is fractions of a cent in the first few minutes and reads
 * as "$0.00" otherwise. 4 decimals keeps small amounts legible
 * without drifting into micro-cent noise.
 */
function formatUSD(value: number): string {
  if (!Number.isFinite(value)) return "$0.0000";
  return `$${value.toFixed(4)}`;
}

/** Local copy to avoid pulling compactNumber cross-module for a chart. */
function compactNumber(n: number): string {
  const abs = Math.abs(n);
  if (abs >= 1e9) return `${(n / 1e9).toFixed(1).replace(/\.0$/, "")}B`;
  if (abs >= 1e6) return `${(n / 1e6).toFixed(1).replace(/\.0$/, "")}M`;
  if (abs >= 1e3) return `${(n / 1e3).toFixed(1).replace(/\.0$/, "")}k`;
  return String(n);
}

export type { CostPoint };
