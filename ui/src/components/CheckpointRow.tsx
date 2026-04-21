import type { Checkpoint } from "@/hooks/useCheckpoints";
import { relativeTime } from "@/lib/format";
import { cn } from "@/lib/utils";

interface CheckpointRowProps {
  checkpoint: Checkpoint;
  onSelect: (cp: Checkpoint) => void;
  selected?: boolean;
}

function shortSha(cp: Checkpoint): string {
  return cp.short_sha && cp.short_sha.length > 0 ? cp.short_sha : cp.sha.slice(0, 7);
}

/**
 * V17 row — clickable checkpoint surface; opens the RevertSheet on click.
 * Layout: short SHA | subject | relative time. Full SHA is preserved on
 * the underlying Checkpoint and forwarded to the revert API.
 */
export function CheckpointRow({
  checkpoint,
  onSelect,
  selected,
}: CheckpointRowProps) {
  return (
    <button
      type="button"
      onClick={() => onSelect(checkpoint)}
      aria-pressed={selected}
      className={cn(
        "flex w-full items-baseline gap-3 border-b border-border px-4 py-3 text-left",
        "transition-colors hover:bg-surface-2",
        selected && "bg-surface-2",
      )}
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
  );
}
