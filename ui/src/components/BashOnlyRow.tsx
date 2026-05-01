import { useState } from "react";
import type { ToolCallRow as ToolCallRowType } from "@/hooks/useFeed";
import { stripAnsi } from "@/lib/format";
import { cn } from "@/lib/utils";

interface BashOnlyRowProps {
  row: ToolCallRowType;
}

const CMD_MAX = 120;
const OUTPUT_LINES = 8;

function truncate(s: string, max: number): string {
  if (s.length <= max) return s;
  return s.slice(0, max - 1) + "…";
}

/**
 * Compact one-liner for the Feed-tab "Bash" filter (V10).
 *
 * - Mono command on the left (single-line, truncated ~120 chars).
 * - Right-aligned status chip: `ok` (sage) on success, `err <n>` (ember)
 *   on failure. Success = exit_code === 0 OR (exit_code undefined AND
 *   !is_error). Anything else is an error.
 * - Click toggles a small expansion with the full command + first
 *   OUTPUT_LINES lines of stripped-ANSI output.
 */
export function BashOnlyRow({ row }: BashOnlyRowProps) {
  const [open, setOpen] = useState(false);

  const cmdFull = stripAnsi(row.input ?? "").replaceAll(/\s+/g, " ").trim();
  const cmdLine = truncate(cmdFull, CMD_MAX);

  const hasExit = typeof row.exit_code === "number";
  const isError = row.is_error || (hasExit && row.exit_code !== 0);
  const chipLabel = isError
    ? `err${hasExit ? ` ${row.exit_code}` : ""}`
    : "ok";

  const outputLines = row.summary
    ? stripAnsi(row.summary).split(/\r?\n/).slice(0, OUTPUT_LINES)
    : [];

  return (
    <article
      className={cn(
        "border-l-[3px] border-b border-b-border px-4 py-2",
        isError ? "border-l-alert-ember bg-alert-ember/5" : "border-l-transparent",
      )}
      data-testid="bash-row"
    >
      <button
        type="button"
        aria-expanded={open}
        aria-label={`Bash command: ${cmdFull}`}
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-3 text-left"
      >
        <code
          className="min-w-0 flex-1 truncate font-mono text-xs text-fg"
          title={cmdFull}
        >
          {cmdLine}
        </code>
        <span
          data-testid="bash-chip"
          data-status={isError ? "err" : "ok"}
          className={cn(
            "shrink-0 rounded-sm px-1.5 py-0.5 font-mono text-[10px] font-semibold uppercase tracking-[0.12em] tabular-nums",
            isError
              ? "bg-alert-ember/15 text-alert-ember"
              : "bg-live-dot/15 text-live-dot",
          )}
        >
          {chipLabel}
        </span>
      </button>
      {open && (
        <div className="mt-2 space-y-1 border-t border-border pt-2">
          <pre
            data-testid="bash-expanded-cmd"
            className="max-w-full overflow-x-auto whitespace-pre-wrap break-words font-mono text-xs text-fg-muted"
          >
            {cmdFull}
          </pre>
          {outputLines.length > 0 && (
            <pre
              data-testid="bash-expanded-output"
              className={cn(
                "max-w-full overflow-x-auto whitespace-pre-wrap break-words font-mono text-xs",
                isError ? "text-alert-ember" : "text-fg-dim",
              )}
            >
              {outputLines.join("\n")}
            </pre>
          )}
        </div>
      )}
    </article>
  );
}
