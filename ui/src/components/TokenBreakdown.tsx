import { compactNumber } from "@/lib/format";
import { cn } from "@/lib/utils";
import type { TokenUsage } from "@/hooks/useSessions";

interface TokenBreakdownProps {
  tokens?: TokenUsage;
  className?: string;
}

/**
 * Live per-session token triad — input / output / cache — sourced from
 * the statusline dump's `context_window.current_usage`. Values reflect
 * the last completed turn, not session totals, so they move with every
 * quota_update SSE event.
 */
export function TokenBreakdown({ tokens, className }: TokenBreakdownProps) {
  return (
    <div
      role="group"
      aria-label="Live token usage"
      className={cn(
        "flex items-center gap-6 border-b border-border bg-bg px-4 py-2",
        className,
      )}
    >
      <Cell label="In" value={tokens?.input_tokens} />
      <Cell label="Out" value={tokens?.output_tokens} />
      <Cell label="Cache" value={tokens?.cache_tokens} />
    </div>
  );
}

function Cell({ label, value }: { label: string; value?: number }) {
  const known = typeof value === "number";
  return (
    <div className="flex items-baseline gap-2">
      <span className="text-[10px] font-semibold uppercase tracking-[0.18em] text-fg-muted">
        {label}
      </span>
      <span
        className={cn(
          "font-mono text-sm tabular-nums",
          known ? "text-fg" : "text-fg-dim",
        )}
        title={known ? value.toLocaleString() : undefined}
      >
        {known ? compactNumber(value) : "—"}
      </span>
    </div>
  );
}
