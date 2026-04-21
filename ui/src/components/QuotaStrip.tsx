import { useQuota } from "@/hooks/useQuota";
import { relativeTime } from "@/lib/format";
import { cn } from "@/lib/utils";

interface BarProps {
  pct?: number;
  resetAt?: string;
  label: string;
}

function pctColor(pct: number): string {
  if (pct >= 90) return "bg-alert-ember";
  if (pct >= 75) return "bg-accent-gold";
  return "bg-fg-muted";
}

function QuotaBar({ pct, resetAt, label }: BarProps) {
  const known = typeof pct === "number";
  const safePct = known ? Math.max(0, Math.min(100, pct)) : 0;
  return (
    <div className="flex min-w-0 flex-1 items-center gap-3">
      <span className="shrink-0 text-[10px] font-semibold uppercase tracking-[0.18em] text-fg-muted">
        {label}
      </span>
      <div
        className="relative h-1.5 min-w-[6rem] flex-1 overflow-hidden rounded-full bg-surface-2"
        role="progressbar"
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={known ? safePct : undefined}
        aria-label={`${label} quota usage`}
      >
        {known && (
          <div
            className={cn("h-full transition-all", pctColor(safePct))}
            style={{ width: `${safePct}%` }}
          />
        )}
      </div>
      <span
        className={cn(
          "shrink-0 font-mono text-xs tabular-nums",
          known ? "text-fg" : "text-fg-dim",
        )}
      >
        {known ? `${Math.round(safePct)}%` : "—"}
      </span>
      {resetAt && (
        <span
          className="hidden shrink-0 text-xs text-fg-dim md:inline"
          title={`Resets at ${resetAt}`}
        >
          resets in {relativeTime(resetAt)}
        </span>
      )}
    </div>
  );
}

/**
 * V12 — persistent strip showing weekly + 5-hour quota usage. Sourced
 * from the SSE-fed `useQuota` cache; renders "—" placeholders before
 * the first quota_update event arrives.
 */
export function QuotaStrip() {
  const { data } = useQuota();
  const pct5 = data?.five_hr_pct;
  const pctW = data?.weekly_pct;
  const reset5 = data?.five_hr_resets_at;
  const resetW = data?.weekly_resets_at;

  return (
    <div
      role="region"
      aria-label="Rate limit usage"
      className="flex flex-wrap items-center gap-x-6 gap-y-2 border-b border-border bg-bg px-4 py-2"
    >
      <QuotaBar pct={pct5} resetAt={reset5} label="5h" />
      <QuotaBar pct={pctW} resetAt={resetW} label="Weekly" />
    </div>
  );
}
