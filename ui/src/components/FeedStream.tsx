import { useMemo } from "react";
import { useFeed } from "@/hooks/useFeed";
import { ToolCallRow } from "@/components/ToolCallRow";
import { BashOnlyRow } from "@/components/BashOnlyRow";
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
}: FeedStreamProps) {
  const { data } = useFeed(sessionName);

  const rows = useMemo(() => {
    const source = data ?? [];
    const filtered = bashOnly
      ? source.filter((r) => r.tool === "Bash")
      : source;
    const slice = filtered.slice(-limit);
    return slice.slice().reverse();
  }, [data, limit, bashOnly]);

  const emptyMessage = bashOnly
    ? "No Bash commands yet."
    : "Waiting for the first tool call…";

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
    </section>
  );
}
