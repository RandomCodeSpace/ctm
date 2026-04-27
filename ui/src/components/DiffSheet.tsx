import { useMemo } from "react";
import { Loader2 } from "lucide-react";
import { Drawer, Button } from "@ossrandom/design-system";
import type { Checkpoint } from "@/hooks/useCheckpoints";
import { useCheckpointDiff } from "@/hooks/useCheckpoints";
import { relativeTime } from "@/lib/format";
import { cn } from "@/lib/utils";
import { classifyLine } from "@/lib/diff";

interface DiffSheetProps {
  sessionName: string;
  checkpoint: Checkpoint | null;
  onClose: () => void;
}

/**
 * V18 standalone diff viewer.
 *
 * Fetches `/api/sessions/:name/checkpoints/:sha/diff` (unified `git
 * show` output, text/plain) and renders it in a scrollable monospace
 * <pre>. Colouring is the minimal set that preserves legibility
 * without a full syntax-highlighter dependency:
 *
 *   +...   emerald  (added line)
 *   -...   alert-ember  (removed line)
 *   @@...  fg-dim  (hunk header)
 *   else   fg
 *
 * Slide-out chrome, focus trap, ESC-to-close come from the design-system
 * Drawer component — same primitive used by RevertSheet for consistency.
 */
export function DiffSheet({ sessionName, checkpoint, onClose }: DiffSheetProps) {
  const open = checkpoint !== null;
  const {
    data: diff,
    isLoading,
    isError,
    error,
  } = useCheckpointDiff(sessionName, checkpoint?.sha);

  const lines = useMemo(() => (diff ? diff.split("\n") : []), [diff]);

  return (
    <Drawer
      open={open}
      onClose={onClose}
      placement="right"
      width="min(100vw, 42rem)"
      title="Checkpoint diff"
      description={
        <>
          Unified diff from <code className="font-mono text-xs">git show</code>.
        </>
      }
      footer={
        <Button variant="primary" size="sm" onClick={onClose}>
          Close
        </Button>
      }
    >
      {checkpoint && (
        <>
          <div className="shrink-0 space-y-1 px-4 py-3 border-b border-border">
            <p className="font-mono text-xs text-accent-gold">
              {checkpoint.short_sha || checkpoint.sha.slice(0, 7)}
            </p>
            <p className="text-sm text-fg">{checkpoint.subject}</p>
            <p className="text-xs text-fg-dim">
              <time dateTime={checkpoint.ts}>{relativeTime(checkpoint.ts)}</time>
            </p>
          </div>

          <div className="min-h-0 flex-1 overflow-auto">
            {isLoading && (
              <p
                className="flex items-center gap-2 p-4 text-sm text-fg-muted"
                aria-live="polite"
              >
                <Loader2 size={14} className="animate-spin" aria-hidden />
                Loading diff…
              </p>
            )}
            {isError && (
              <p
                role="alert"
                className="m-4 border-l-[3px] border-alert-ember bg-alert-ember/5 p-3 text-sm text-alert-ember"
              >
                Could not load diff
                {error instanceof Error ? `: ${error.message}` : ""}
              </p>
            )}
            {!isLoading && !isError && diff !== undefined && (
              <pre
                data-testid="diff-pre"
                className="whitespace-pre font-mono text-[12px] leading-5 p-4 text-fg"
              >
                {lines.map((line, i) => (
                  <DiffLine key={i} line={line} />
                ))}
              </pre>
            )}
          </div>
        </>
      )}
    </Drawer>
  );
}

interface DiffLineProps {
  line: string;
}

function DiffLine({ line }: DiffLineProps) {
  const cls = classifyLine(line);
  return (
    <div className={cn("whitespace-pre", cls)}>{line === "" ? " " : line}</div>
  );
}

export { classifyLine };
