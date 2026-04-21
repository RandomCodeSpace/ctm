import { useMemo, useState } from "react";
import { ChevronRight, Loader2 } from "lucide-react";
import type { ToolCallRow as ToolCallRowType } from "@/hooks/useFeed";
import { toolDescriptor } from "@/lib/tools";
import { relativeTime, stripAnsi } from "@/lib/format";
import { cn } from "@/lib/utils";
import { classifyLine } from "@/lib/diff";
import { useToolCallDetail } from "@/hooks/useToolCallDetail";

interface ToolCallRowProps {
  row: ToolCallRowType;
  /** When true, prefix the row with the session name in dim text. */
  showSession?: boolean;
}

const INPUT_MAX = 240;

function previewInput(s: string | undefined): string {
  if (!s) return "";
  const stripped = stripAnsi(s).replace(/\s+/g, " ").trim();
  if (stripped.length <= INPUT_MAX) return stripped;
  return stripped.slice(0, INPUT_MAX - 1) + "…";
}

// Tools whose hook payload we render as an inline diff snippet.
// Kept in sync with the server-side switch in renderDiff() (see
// internal/serve/api/tool_call_detail.go).
const DIFFABLE_TOOLS = new Set(["Edit", "MultiEdit", "Write"]);

/**
 * Editorial feed row (spec §3 "Feed row, locked: A Editorial").
 *
 * 60px serif small-caps timestamp left, content right (tool name as mini
 * headline, mono input, dim summary). Errors get a 3px ember-red
 * border-l + ember-red summary text.
 *
 * V9 — Edit / MultiEdit / Write rows also get an expand chevron; on
 * first expand we fetch /tool_calls/:id/detail and render the
 * server-rendered unified-diff snippet below the row. Non-diff tools
 * render exactly as before; the chevron is not shown.
 */
export function ToolCallRow({ row, showSession }: ToolCallRowProps) {
  const desc = toolDescriptor(row.tool);
  const Icon = desc.icon;
  const isError = row.is_error;
  const canExpand = DIFFABLE_TOOLS.has(row.tool) && Boolean(row.id);

  const [expanded, setExpanded] = useState(false);
  const { data: detail, isFetching, isError: detailErr, error } =
    useToolCallDetail(row.session, row.id, expanded);

  const diffLines = useMemo(
    () => (detail?.diff ? detail.diff.split("\n") : []),
    [detail?.diff],
  );

  return (
    <article
      className={cn(
        "ctm-row-in flex gap-4 border-l-[3px] border-transparent px-4 py-3 border-b border-b-border",
        isError && "border-l-alert-ember bg-alert-ember/5",
      )}
    >
      <time
        dateTime={row.ts}
        className="w-[60px] shrink-0 pt-0.5 font-serif text-[11px] uppercase tracking-[0.18em] text-fg-dim"
        title={row.ts}
      >
        {relativeTime(row.ts)}
      </time>
      <div className="min-w-0 flex-1 space-y-1">
        <div className="flex items-center gap-2">
          <Icon size={14} aria-hidden className={desc.colorClass} />
          <span className="text-sm font-semibold text-fg">{row.tool}</span>
          {showSession && (
            <span className="font-mono text-[11px] text-fg-dim">
              · {row.session}
            </span>
          )}
          {canExpand && (
            <button
              type="button"
              data-testid="tool-expand"
              aria-expanded={expanded}
              aria-label={expanded ? "Collapse diff" : "Expand diff"}
              onClick={() => setExpanded((v) => !v)}
              className={cn(
                "ml-auto inline-flex items-center gap-1 rounded-sm px-1.5 py-0.5",
                "text-[10px] uppercase tracking-[0.18em] text-fg-dim",
                "hover:bg-surface-2 hover:text-fg",
                "focus:outline-none focus-visible:ring-1 focus-visible:ring-accent-gold",
              )}
            >
              <ChevronRight
                size={12}
                aria-hidden
                className={cn(
                  "transition-transform",
                  expanded && "rotate-90",
                )}
              />
              diff
            </button>
          )}
        </div>
        {row.input && (
          <pre className="max-w-full overflow-x-auto whitespace-pre-wrap break-words font-mono text-xs text-fg-muted">
            {previewInput(row.input)}
          </pre>
        )}
        {row.summary && (
          <p
            className={cn(
              "font-mono text-xs",
              isError ? "text-alert-ember" : "text-fg-dim",
            )}
          >
            {previewInput(row.summary)}
          </p>
        )}

        {expanded && canExpand && (
          <div
            data-testid="tool-detail"
            className="mt-2 rounded-sm border border-border bg-surface-2/40"
          >
            {isFetching && !detail && (
              <p
                className="flex items-center gap-2 px-3 py-2 text-xs text-fg-muted"
                aria-live="polite"
              >
                <Loader2 size={12} className="animate-spin" aria-hidden />
                Loading diff…
              </p>
            )}
            {detailErr && (
              <p
                role="alert"
                className="border-l-[3px] border-alert-ember bg-alert-ember/5 px-3 py-2 text-xs text-alert-ember"
              >
                Could not load diff
                {error instanceof Error ? `: ${error.message}` : ""}
              </p>
            )}
            {detail && detail.diff && (
              <pre
                data-testid="tool-diff"
                className="max-w-full overflow-x-auto whitespace-pre px-3 py-2 font-mono text-[11px] leading-5 text-fg"
              >
                {diffLines.map((line, i) => (
                  <div
                    key={i}
                    className={cn("whitespace-pre", classifyLine(line))}
                  >
                    {line === "" ? " " : line}
                  </div>
                ))}
              </pre>
            )}
            {detail && !detail.diff && (
              <p className="px-3 py-2 text-xs text-fg-dim">
                No diff available for this tool call.
              </p>
            )}
          </div>
        )}
      </div>
    </article>
  );
}
