import type { Attention } from "@/hooks/useSessions";
import { cn } from "@/lib/utils";

const HUMAN: Record<string, string> = {
  error_burst: "Error burst",
  stalled: "Stalled",
  quota_low: "Quota low",
  permission_request: "Permission",
  context_high: "Context high",
  long_session: "Long session",
  tmux_dead: "Tmux dead",
};

function humanize(state: string): string {
  return HUMAN[state] ?? state.replace(/_/g, " ");
}

interface AttentionLabelProps {
  attention: Attention;
  className?: string;
}

/**
 * Small uppercase ember-red label rendered below the metadata row when
 * attention.state is non-clear. Multiple labels stack via parent layout.
 * Spec §3 (Attention treatment, locked: B Halftone).
 */
export function AttentionLabel({ attention, className }: AttentionLabelProps) {
  if (attention.state === "clear") return null;
  return (
    <div
      role="status"
      className={cn(
        "flex flex-wrap items-baseline gap-x-2 text-[11px] font-semibold uppercase tracking-[0.18em] text-alert-ember",
        className,
      )}
    >
      <span>{humanize(attention.state)}</span>
      {attention.details && (
        <span className="text-fg-muted normal-case tracking-normal text-xs font-normal">
          {attention.details}
        </span>
      )}
    </div>
  );
}
