import { useMemo, useState } from "react";
import { ChevronRight } from "lucide-react";
import { Skeleton } from "@ossrandom/design-system";
import { useSubagents, type SubagentNode } from "@/hooks/useSubagents";
import { relativeTime } from "@/lib/format";
import { cn } from "@/lib/utils";

/**
 * V15 — Subagent tree for a single session.
 *
 * Renders as an indent-based list. Each row is one subagent:
 *
 *   [status-dot] <agent_type> · <description>    <elapsed>
 *
 * Clicking a row expands it to show its tool-call count and, once we
 * wire per-subagent tool-call detail, a nested list of ToolCallRows.
 * For v0.2 the expanded section shows only the aggregate counters
 * we already have — the full per-agent tool_call REST endpoint is
 * tracked as a follow-up (see teams.go header).
 *
 * Refetches on any `subagent_start` SSE event (handled by
 * SseProvider invalidating the queryKey).
 *
 * Empty / error states mirror the CheckpointsTab conventions used
 * elsewhere in SessionDetail so the visual language stays consistent.
 */
export function SubagentTree({ sessionName }: { sessionName: string }) {
  const { data, isLoading, isError, error } = useSubagents(sessionName);
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});

  // Today every node is a root (no parent_id in the JSONL). Pre-sort
  // by started_at descending so the render loop doesn't care about
  // ordering — the server already returns newest-first but we defensive-
  // sort in case a future server tweak reorders.
  const rows = useMemo<SubagentNode[]>(() => {
    const src = data?.subagents ?? [];
    const copy = src.slice();
    copy.sort((a, b) => b.started_at.localeCompare(a.started_at));
    return copy;
  }, [data?.subagents]);

  function toggle(id: string) {
    setExpanded((prev) => ({ ...prev, [id]: !prev[id] }));
  }

  return (
    <section
      aria-label="Subagent tree"
      className="min-h-0 flex-1 overflow-y-auto"
    >
      {isLoading && (
        <div className="space-y-2 p-4">
          <Skeleton className="h-8 w-full" />
          <Skeleton className="h-8 w-4/5" />
        </div>
      )}

      {isError && (
        <p
          role="alert"
          className="m-4 border-l-[3px] border-alert-ember bg-surface px-3 py-2 text-sm text-alert-ember"
        >
          Could not load subagents
          {error instanceof Error ? `: ${error.message}` : ""}
        </p>
      )}

      {!isLoading && !isError && rows.length === 0 && (
        <p className="px-4 py-8 text-center text-sm text-fg-dim">
          No subagents for this session.
        </p>
      )}

      <ul role="list" className="divide-y divide-border">
        {rows.map((node) => (
          <SubagentRow
            key={node.id}
            node={node}
            isExpanded={Boolean(expanded[node.id])}
            onToggle={() => toggle(node.id)}
          />
        ))}
      </ul>
    </section>
  );
}

function SubagentRow({
  node,
  isExpanded,
  onToggle,
}: {
  node: SubagentNode;
  isExpanded: boolean;
  onToggle: () => void;
}) {
  const label = node.type || "subagent";
  return (
    <li>
      <button
        type="button"
        onClick={onToggle}
        aria-expanded={isExpanded}
        data-testid={`subagent-row-${node.id}`}
        className={cn(
          "flex w-full items-center gap-3 px-4 py-3 text-left",
          "hover:bg-surface-2 focus-visible:bg-surface-2",
          "focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent-gold",
        )}
      >
        <ChevronRight
          size={14}
          aria-hidden
          className={cn(
            "shrink-0 text-fg-muted transition-transform",
            isExpanded && "rotate-90",
          )}
        />
        <StatusDot status={node.status} />
        <span
          className="shrink-0 text-[11px] font-semibold uppercase tracking-[0.18em] text-fg-muted"
          data-testid="subagent-type"
        >
          {label}
        </span>
        <span className="min-w-0 flex-1 truncate text-sm text-fg">
          {node.description || <span className="text-fg-dim">—</span>}
        </span>
        <time
          dateTime={node.started_at}
          className="shrink-0 font-mono text-[11px] tabular-nums text-fg-dim"
        >
          {relativeTime(node.started_at)} ago
        </time>
      </button>
      {isExpanded && (
        <div
          data-testid={`subagent-detail-${node.id}`}
          className="border-t border-border bg-surface px-6 py-3 text-sm text-fg-muted"
        >
          <dl className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-1">
            <dt className="text-[11px] font-semibold uppercase tracking-[0.18em]">
              tool calls
            </dt>
            <dd className="font-mono tabular-nums text-fg">
              {node.tool_calls}
            </dd>
            <dt className="text-[11px] font-semibold uppercase tracking-[0.18em]">
              status
            </dt>
            <dd className="text-fg">{node.status}</dd>
            {node.stopped_at && (
              <>
                <dt className="text-[11px] font-semibold uppercase tracking-[0.18em]">
                  stopped
                </dt>
                <dd>
                  <time dateTime={node.stopped_at}>
                    {relativeTime(node.stopped_at)} ago
                  </time>
                </dd>
              </>
            )}
          </dl>
        </div>
      )}
    </li>
  );
}

/**
 * Status dot — matches the V15 brief:
 *   running   → pulsing live-dot
 *   completed → solid fg-muted check
 *   failed    → solid alert-ember
 *
 * Kept as a span (not a svg) so we don't drag in a new icon for one
 * rectangle; the CSS tokens already defined in index.css do the
 * heavy lifting.
 */
function StatusDot({ status }: { status: SubagentNode["status"] }) {
  const label =
    status === "running"
      ? "Running"
      : status === "failed"
      ? "Failed"
      : "Completed";
  return (
    <span
      role="img"
      aria-label={label}
      data-testid={`status-dot-${status}`}
      className={cn(
        "inline-block h-2.5 w-2.5 shrink-0 rounded-full",
        status === "running" && "animate-pulse bg-live-dot",
        status === "completed" && "bg-fg-muted",
        status === "failed" && "bg-alert-ember",
      )}
    />
  );
}
