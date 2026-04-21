import { useMemo } from "react";
import { Loader2 } from "lucide-react";
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet";
import { Button } from "@/components/ui/button";
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
 * The sheet's slide-out chrome, focus trap, and ESC-to-close are
 * provided by the underlying Radix Sheet primitive — matching
 * RevertSheet for consistency.
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
    <Sheet open={open} onOpenChange={(v) => !v && onClose()}>
      <SheetContent
        side="right"
        className="bg-surface text-fg w-full sm:max-w-2xl border-l border-border flex flex-col"
      >
        {checkpoint && (
          <>
            <SheetHeader className="border-b border-border">
              <SheetTitle className="font-serif text-xl text-fg">
                Checkpoint diff
              </SheetTitle>
              <SheetDescription className="text-fg-muted">
                Unified diff from <code className="font-mono text-xs text-accent-gold">git show</code>.
              </SheetDescription>
            </SheetHeader>

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

            <SheetFooter className="border-t border-border">
              <Button
                type="button"
                onClick={onClose}
                className="bg-accent-gold text-bg hover:opacity-90"
              >
                Close
              </Button>
            </SheetFooter>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}

interface DiffLineProps {
  line: string;
}

/**
 * Single-line render. `+++` / `---` are file-header lines (part of
 * the `diff --git` preamble), not content — we intentionally leave
 * them in the "added/removed" colour buckets because that's the
 * convention users expect from git CLI output.
 */
function DiffLine({ line }: DiffLineProps) {
  const cls = classifyLine(line);
  return (
    <div className={cn("whitespace-pre", cls)}>{line === "" ? " " : line}</div>
  );
}

// Re-export the shared classifier so existing imports
// (`DiffSheet.test.tsx`, potential future consumers) keep working
// without touching the public surface.
export { classifyLine };
