import type { Checkpoint } from "@/hooks/useCheckpoints";
import { relativeTime } from "@/lib/format";
import { cn } from "@/lib/utils";

interface CheckpointRowProps {
  checkpoint: Checkpoint;
  onSelect: (cp: Checkpoint) => void;
  /** V18: opens the DiffSheet for this checkpoint. */
  onViewDiff?: (cp: Checkpoint) => void;
  selected?: boolean;
}

function shortSha(cp: Checkpoint): string {
  return cp.short_sha && cp.short_sha.length > 0 ? cp.short_sha : cp.sha.slice(0, 7);
}

/**
 * V17 row — clickable checkpoint surface; opens the RevertSheet on click.
 * Layout: short SHA | subject | relative time | [View diff].
 *
 * V18 adds a sibling `View diff` button whose onClick stops propagation
 * so the row's primary "open revert sheet" affordance is preserved.
 */
export function CheckpointRow({
  checkpoint,
  onSelect,
  onViewDiff,
  selected,
}: CheckpointRowProps) {
  return (
    <div
      className={cn(
        "group flex w-full items-baseline gap-3 border-b border-border px-4 py-3",
        "transition-colors hover:bg-surface-2",
        selected && "bg-surface-2",
      )}
    >
      <button
        type="button"
        onClick={() => onSelect(checkpoint)}
        aria-pressed={selected}
        className="flex min-w-0 flex-1 items-baseline gap-3 text-left bg-transparent"
      >
        <code className="w-[60px] shrink-0 font-mono text-xs text-accent-gold">
          {shortSha(checkpoint)}
        </code>
        <span className="min-w-0 flex-1 truncate text-sm text-fg">
          {checkpoint.subject}
        </span>
        <time
          dateTime={checkpoint.ts}
          className="shrink-0 font-mono text-xs text-fg-dim"
          title={checkpoint.ts}
        >
          {relativeTime(checkpoint.ts)}
        </time>
      </button>
      {onViewDiff && (
        <button
          type="button"
          onClick={(e) => {
            e.stopPropagation();
            onViewDiff(checkpoint);
          }}
          className={cn(
            "shrink-0 rounded border border-border bg-transparent px-2 py-1",
            "text-[10px] font-semibold uppercase tracking-[0.18em] text-fg-muted",
            "transition-colors hover:bg-surface hover:text-fg",
            "focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent-gold",
          )}
        >
          View diff
        </button>
      )}
    </div>
  );
}
