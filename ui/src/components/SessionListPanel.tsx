import { useMemo, useState } from "react";
import { Link } from "react-router";
import { Skeleton } from "@/components/ui/skeleton";
import { SessionCard } from "@/components/SessionCard";
import {
  sortSessions,
  useSessions,
  type Session,
} from "@/hooks/useSessions";
import { cn } from "@/lib/utils";

interface SessionListPanelProps {
  /** When set, the matching card highlights as the active two-pane selection. */
  activeName?: string;
  className?: string;
}

/**
 * V1 session list. Active sessions by default; "Show all" toggle reveals
 * stored-but-dead too. Sort: attention severity DESC, last_attached_at DESC.
 */
export function SessionListPanel({
  activeName,
  className,
}: SessionListPanelProps) {
  const [showAll, setShowAll] = useState(false);
  const { data, isLoading, isError, error } = useSessions();

  const visible = useMemo<Session[]>(() => {
    const all = data ?? [];
    const filtered = showAll ? all : all.filter((s) => s.is_active);
    return filtered.slice().sort(sortSessions);
  }, [data, showAll]);

  return (
    <aside
      aria-label="Sessions"
      className={cn(
        "flex h-full min-h-0 flex-col border-r border-border bg-bg",
        className,
      )}
    >
      <header className="flex items-center justify-between border-b border-border px-4 py-3">
        <span className="text-[11px] font-semibold uppercase tracking-[0.18em] text-fg-muted">
          Sessions
        </span>
        <label className="flex cursor-pointer items-center gap-2 text-xs text-fg-dim">
          <input
            type="checkbox"
            checked={showAll}
            onChange={(e) => setShowAll(e.target.checked)}
            className="h-3 w-3 cursor-pointer accent-accent-gold"
          />
          Show all
        </label>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {isLoading && (
          <div className="space-y-2 p-4">
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-16 w-full" />
            <Skeleton className="h-16 w-full" />
          </div>
        )}

        {isError && (
          <p
            role="alert"
            className="m-4 border-l-[3px] border-alert-ember bg-surface px-3 py-2 text-sm text-alert-ember"
          >
            Could not load sessions
            {error instanceof Error ? `: ${error.message}` : ""}
          </p>
        )}

        {!isLoading && !isError && visible.length === 0 && (
          <p className="px-4 py-8 text-center text-sm text-fg-dim">
            {showAll
              ? "No sessions on record."
              : "No active sessions. Start one with ctm new or ctm yolo."}
          </p>
        )}

        <ul role="list">
          {visible.map((s) => (
            <li key={s.name}>
              <SessionCard session={s} active={s.name === activeName} />
            </li>
          ))}
        </ul>
      </div>

      <footer className="border-t border-border px-4 py-2 text-xs">
        <Link
          to="/feed"
          className="text-fg-muted hover:text-fg transition-colors"
        >
          Live feed (all sessions) &rarr;
        </Link>
      </footer>
    </aside>
  );
}
