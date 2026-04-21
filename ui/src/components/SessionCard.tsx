import { Link } from "react-router";
import { isStale, type Session } from "@/hooks/useSessions";
import { HealthDot, healthState } from "@/components/HealthDot";
import { AttentionLabel } from "@/components/AttentionLabel";
import { compactNumber, relativeTime, shortenPath } from "@/lib/format";
import { cn } from "@/lib/utils";

interface SessionCardProps {
  session: Session;
  active?: boolean;
}

/**
 * V1 session card. Layout:
 *
 *   <HealthDot>  name              UPPERCASE-MODE
 *                workdir (mono)                    nN%   17 min
 *                <AttentionLabel> rows
 *
 * When attention.state !== "clear", the card grows a 3px ember-red
 * left border (Halftone treatment, spec §3).
 */
export function SessionCard({ session, active }: SessionCardProps) {
  const attn = session.attention;
  const attentive = Boolean(attn && attn.state !== "clear");
  const state = healthState({
    tmux_alive: session.tmux_alive,
    last_tool_call_at: session.last_tool_call_at,
  });
  // Primary age display prefers live tool-call activity so the card's
  // "last X" matches the list sort; falls back through last-attached
  // and created-at when the engine hasn't seen a tool call yet.
  const lastTC = session.last_tool_call_at;
  const lastAttach = session.last_attached_at ?? session.created_at;
  const primary = lastTC ?? lastAttach;
  const primaryLabel = lastTC ? "last tool call" : "last attached";

  return (
    <Link
      to={`/s/${encodeURIComponent(session.name)}`}
      aria-current={active ? "page" : undefined}
      data-attentive={attentive || undefined}
      className={cn(
        "group block border-l-[3px] border-transparent bg-surface px-4 py-3",
        "border-b border-b-border",
        "transition-colors hover:bg-surface-2",
        attentive && "border-l-alert-ember",
        active && "bg-surface-2",
      )}
    >
      <div className="flex items-start justify-between gap-3">
        <div className="flex min-w-0 items-center gap-2">
          <HealthDot state={state} />
          <span className="truncate font-medium text-fg">{session.name}</span>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          {isStale(session) && (
            <span
              className="text-[10px] font-semibold uppercase tracking-[0.18em] text-accent-gold/80"
              title="No tool call in > 30 min"
              aria-label="stale session"
            >
              stale
            </span>
          )}
          <span
            className={cn(
              "text-[10px] font-semibold uppercase tracking-[0.18em]",
              session.mode === "yolo" ? "text-mode-yolo" : "text-mode-safe",
            )}
          >
            {session.mode}
          </span>
        </div>
      </div>

      <div className="mt-1 flex items-center justify-between gap-3 text-xs">
        <code
          className="truncate font-mono text-fg-dim"
          title={session.workdir}
        >
          {shortenPath(session.workdir, 3)}
        </code>
        <div className="flex shrink-0 items-center gap-3 text-fg-dim">
          {typeof session.context_pct === "number" && (
            <span className="font-mono tabular-nums">
              {Math.round(session.context_pct)}%
            </span>
          )}
          {lastTC && (
            <time
              dateTime={lastTC}
              title={`last tool call ${lastTC}`}
              className="font-mono tabular-nums text-fg"
              aria-label="last tool call"
            >
              <span aria-hidden className="text-fg-muted">⏵</span>{" "}
              {relativeTime(lastTC)}
            </time>
          )}
          <time
            dateTime={primary}
            title={`${primaryLabel} ${primary}`}
            className={cn(lastTC && "text-fg-muted")}
          >
            {lastTC ? relativeTime(lastAttach) : relativeTime(primary)}
          </time>
        </div>
      </div>

      {session.tokens && (
        <div
          className="mt-1 flex items-center gap-3 font-mono text-[10px] tabular-nums text-fg-dim"
          aria-label="Live token usage"
          title={`input ${session.tokens.input_tokens.toLocaleString()} · output ${session.tokens.output_tokens.toLocaleString()} · cache ${session.tokens.cache_tokens.toLocaleString()}`}
        >
          <TokenBlip glyph="↑" color="#0087ff" value={session.tokens.input_tokens} />
          <TokenBlip glyph="↓" color="#00afaf" value={session.tokens.output_tokens} />
          <TokenBlip
            glyph="⚡"
            color="var(--accent-gold)"
            value={session.tokens.cache_tokens}
          />
        </div>
      )}

      {attn && attn.state !== "clear" && (
        <div className="mt-2">
          <AttentionLabel attention={attn} />
        </div>
      )}
    </Link>
  );
}

function TokenBlip({
  glyph,
  color,
  value,
}: {
  glyph: string;
  color: string;
  value: number;
}) {
  return (
    <span className="flex items-baseline gap-1">
      <span aria-hidden style={{ color, fontWeight: 700 }}>
        {glyph}
      </span>
      <span className="text-fg">{compactNumber(value)}</span>
    </span>
  );
}
