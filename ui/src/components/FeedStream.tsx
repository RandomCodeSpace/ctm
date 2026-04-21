import { useCallback, useMemo, useState } from "react";
import { useFeed, type ToolCallRow as ToolCallRowType } from "@/hooks/useFeed";
import { ToolCallRow } from "@/components/ToolCallRow";
import { BashOnlyRow } from "@/components/BashOnlyRow";
import type { FeedHistoryResponse } from "@/hooks/useFeedHistory";
import { cn } from "@/lib/utils";

interface FeedStreamProps {
  /** When undefined, renders the cross-session feed (V5). */
  sessionName?: string;
  className?: string;
  /** Cap rows displayed (cache is capped at 500 by SseProvider). */
  limit?: number;
  /**
   * V10 — when true, filter to Bash-only tool calls and swap the renderer
   * for the compact <BashOnlyRow> strip. Default false preserves the
   * existing editorial feed used everywhere else.
   */
  bashOnly?: boolean;
  /**
   * V6 — when provided, renders a "Load older" button at the bottom of
   * the list. Clicked with the oldest visible row's cursor; returned
   * events are appended below the ring view. Omitted in the
   * cross-session feed (no per-session cursor semantics).
   *
   * The fetcher is injected rather than called directly so tests can
   * stub without mocking `fetch`. Production callers pass
   * `fetchFeedHistory` from `@/hooks/useFeedHistory`.
   */
  onLoadOlder?: (beforeId: string) => Promise<FeedHistoryResponse>;
}

/**
 * Derive the hub-style cursor id for a cache row. The feed cache
 * stores only payloads (no event id), so we reconstruct the id from
 * the payload timestamp in the same shape the backend uses:
 * `<unix-nano>-0`. Monotonicity within the cursor window is preserved
 * because history results come out of a single JSONL file in
 * append-order.
 */
function cursorFromRow(row: ToolCallRowType): string {
  const ms = Date.parse(row.ts);
  if (Number.isFinite(ms)) {
    const nanos = BigInt(ms) * 1_000_000n;
    return `${nanos.toString()}-0`;
  }
  return "0-0";
}

/**
 * Read-only live feed of tool calls. Reads from the TanStack Query cache
 * populated by SseProvider on `tool_call` events. No own subscription —
 * SseProvider owns the EventSource.
 *
 * Newest first; reverses the cache (which is append-order) for display.
 */
export function FeedStream({
  sessionName,
  className,
  limit = 500,
  bashOnly = false,
  onLoadOlder,
}: FeedStreamProps) {
  const { data } = useFeed(sessionName);

  // Historical rows appended below the ring view. Kept in local state
  // rather than the TanStack cache so live SSE ticks never have to
  // re-sort / de-dup against older rows (see useFeedHistory docs).
  // Append-order matches the backend's newest-first response, so each
  // successive "Load older" batch lands beneath the previous one.
  const [historical, setHistorical] = useState<ToolCallRowType[]>([]);
  const [hasMore, setHasMore] = useState(true);
  const [loading, setLoading] = useState(false);
  const [loadError, setLoadError] = useState<string | null>(null);

  const liveRows = useMemo(() => {
    const source = data ?? [];
    const filtered = bashOnly
      ? source.filter((r) => r.tool === "Bash")
      : source;
    const slice = filtered.slice(-limit);
    return slice.slice().reverse();
  }, [data, limit, bashOnly]);

  const displayHistorical = useMemo(() => {
    if (!bashOnly) return historical;
    return historical.filter((r) => r.tool === "Bash");
  }, [historical, bashOnly]);

  const rows = useMemo(
    () => [...liveRows, ...displayHistorical],
    [liveRows, displayHistorical],
  );

  const handleLoadOlder = useCallback(async () => {
    if (!onLoadOlder || loading) return;
    // Cursor = oldest currently-visible row (live tail or last
    // historical batch). When the feed is empty (e.g. fresh tab, SSE
    // hasn't delivered yet), fall back to "now" so the first click
    // still asks the server for anything older than the present
    // moment — it's a best-effort upper bound.
    const oldest = rows[rows.length - 1];
    const cursor = oldest
      ? cursorFromRow(oldest)
      : `${BigInt(Date.now()) * 1_000_000n}-0`;
    setLoading(true);
    setLoadError(null);
    try {
      const resp = await onLoadOlder(cursor);
      const newRows = resp.events.map((e) => e.payload);
      if (newRows.length > 0) {
        setHistorical((prev) => [...prev, ...newRows]);
      }
      setHasMore(resp.has_more);
    } catch (err) {
      setLoadError(err instanceof Error ? err.message : "failed");
    } finally {
      setLoading(false);
    }
  }, [onLoadOlder, loading, rows]);

  const emptyMessage = bashOnly
    ? "No Bash commands yet."
    : "Waiting for the first tool call…";

  // Render "Load older" whenever a fetcher is provided and the server
  // hasn't reported exhaustion. We intentionally do NOT gate on
  // rows.length > 0: a fresh tab with an empty ring still wants to
  // pull historical rows from disk if the user is curious about older
  // activity. The backend handler returns [] + has_more:false when
  // nothing older is available, which flips the button off naturally.
  const showLoadOlder = Boolean(onLoadOlder) && hasMore;

  return (
    <section
      aria-label={
        sessionName ? `Feed for ${sessionName}` : "Live feed (all sessions)"
      }
      className={cn("min-h-0 flex-1 overflow-y-auto", className)}
    >
      {rows.length === 0 ? (
        <p className="px-4 py-8 text-center text-sm text-fg-dim">
          {emptyMessage}
        </p>
      ) : (
        <ol role="list" className="flex flex-col">
          {rows.map((row, i) => (
            <li key={`${row.ts}-${i}`}>
              {bashOnly ? (
                <BashOnlyRow row={row} />
              ) : (
                <ToolCallRow row={row} showSession={!sessionName} />
              )}
            </li>
          ))}
        </ol>
      )}
      {showLoadOlder && (
        <div className="flex justify-center border-t border-border bg-bg py-3">
          <button
            type="button"
            onClick={handleLoadOlder}
            disabled={loading}
            className={cn(
              "rounded-sm px-3 py-1.5 text-[11px] font-semibold uppercase tracking-[0.18em]",
              "border border-border bg-surface text-fg-muted transition-colors",
              "hover:text-fg hover:border-fg-muted",
              "disabled:cursor-not-allowed disabled:opacity-50",
            )}
          >
            {loading ? "Loading…" : "Load older"}
          </button>
        </div>
      )}
      {loadError && (
        <p role="alert" className="px-4 py-2 text-center text-xs text-alert-ember">
          Could not load older events: {loadError}
        </p>
      )}
    </section>
  );
}
