import type { ToolCallRow as ToolCallRowType } from "@/hooks/useFeed";
import { toolDescriptor } from "@/lib/tools";
import { relativeTime, stripAnsi } from "@/lib/format";
import { cn } from "@/lib/utils";

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
  return stripped.slice(0, INPUT_MAX - 1) + "\u2026";
}

/**
 * Editorial feed row (spec §3 "Feed row, locked: A Editorial").
 *
 * 60px serif small-caps timestamp left, content right (tool name as mini
 * headline, mono input, dim summary). Errors get a 3px ember-red
 * border-l + ember-red summary text.
 */
export function ToolCallRow({ row, showSession }: ToolCallRowProps) {
  const desc = toolDescriptor(row.tool);
  const Icon = desc.icon;
  const isError = row.is_error;

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
      </div>
    </article>
  );
}
