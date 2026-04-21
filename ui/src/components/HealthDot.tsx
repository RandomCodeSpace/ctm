import { cn } from "@/lib/utils";

const RECENT_MS = 60_000;

export type HealthState = "live" | "idle" | "dead";

/**
 * Compute health state from a Session-shaped record.
 * - live: tmux alive AND last tool call within 60s
 * - idle: tmux alive but no recent tool calls
 * - dead: tmux not alive (record persisted, process gone)
 */
export function healthState(input: {
  tmux_alive?: boolean;
  last_tool_call_at?: string;
  now?: Date;
}): HealthState {
  if (!input.tmux_alive) return "dead";
  if (!input.last_tool_call_at) return "idle";
  const now = (input.now ?? new Date()).getTime();
  const last = new Date(input.last_tool_call_at).getTime();
  if (Number.isNaN(last)) return "idle";
  return now - last <= RECENT_MS ? "live" : "idle";
}

const COLOR: Record<HealthState, string> = {
  live: "bg-live-dot",
  idle: "bg-fg-dim",
  dead: "bg-fg-muted opacity-50",
};

const LABEL: Record<HealthState, string> = {
  live: "live",
  idle: "idle",
  dead: "stopped",
};

interface HealthDotProps {
  state: HealthState;
  className?: string;
}

/**
 * Tiny status dot — green when live, dim when idle, muted when stored-but-dead.
 * Spec §3 design tokens: --live-dot, --fg-dim, --fg-muted.
 */
export function HealthDot({ state, className }: HealthDotProps) {
  return (
    <span
      role="status"
      aria-label={`session ${LABEL[state]}`}
      title={LABEL[state]}
      className={cn(
        "inline-block h-1.5 w-1.5 shrink-0 rounded-full",
        COLOR[state],
        className,
      )}
    />
  );
}
