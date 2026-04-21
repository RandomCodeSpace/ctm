import { useEffect, useState } from "react";
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
import {
  ApiError,
  postRevert,
  type RevertDirty,
  type RevertSuccess,
} from "@/lib/api";
import type { Checkpoint } from "@/hooks/useCheckpoints";
import { relativeTime } from "@/lib/format";
import { cn } from "@/lib/utils";

interface RevertSheetProps {
  sessionName: string;
  checkpoint: Checkpoint | null;
  onClose: () => void;
}

type Phase =
  | { kind: "confirm" }
  | { kind: "submitting" }
  | { kind: "dirty"; dirty: string[] }
  | { kind: "stashing" }
  | { kind: "success"; result: RevertSuccess }
  | { kind: "fatal_422"; message: string }
  | { kind: "network"; message: string };

function isDirtyBody(b: unknown): b is RevertDirty {
  return (
    typeof b === "object" &&
    b !== null &&
    Array.isArray((b as { dirty_files?: unknown }).dirty_files)
  );
}

/**
 * V17 Revert flow.
 *
 * 1. POST /revert {sha, stash_first: false}
 *    - 200 → show reverted_to (+ stashed_as if any), close after 2s.
 *    - 409 + dirty_files → show file list + "Stash first then revert".
 *    - 422 → show inline allowlist error (should never happen — we send full SHA).
 *    - network → "Retry" button.
 * 2. On stash, POST again with stash_first: true.
 */
export function RevertSheet({ sessionName, checkpoint, onClose }: RevertSheetProps) {
  const [phase, setPhase] = useState<Phase>({ kind: "confirm" });

  // Reset phase whenever the checkpoint changes (re-open the sheet).
  useEffect(() => {
    if (checkpoint) setPhase({ kind: "confirm" });
  }, [checkpoint]);

  // Auto-close on success after 2s.
  useEffect(() => {
    if (phase.kind !== "success") return;
    const t = setTimeout(onClose, 2000);
    return () => clearTimeout(t);
  }, [phase, onClose]);

  async function send(stashFirst: boolean) {
    if (!checkpoint) return;
    setPhase(stashFirst ? { kind: "stashing" } : { kind: "submitting" });
    try {
      const result = await postRevert(sessionName, {
        sha: checkpoint.sha,
        stash_first: stashFirst,
      });
      setPhase({ kind: "success", result });
    } catch (err) {
      if (err instanceof ApiError) {
        if (err.status === 409 && isDirtyBody(err.body)) {
          setPhase({ kind: "dirty", dirty: err.body.dirty_files });
          return;
        }
        if (err.status === 422) {
          setPhase({
            kind: "fatal_422",
            message: "This checkpoint is no longer in the allowed list. Refresh and try again.",
          });
          return;
        }
        setPhase({ kind: "network", message: err.message });
        return;
      }
      setPhase({
        kind: "network",
        message: err instanceof Error ? err.message : "Network error",
      });
    }
  }

  const open = checkpoint !== null;

  return (
    <Sheet open={open} onOpenChange={(v) => !v && onClose()}>
      <SheetContent
        side="right"
        className="bg-surface text-fg w-full sm:max-w-md border-l border-border"
      >
        {checkpoint && (
          <>
            <SheetHeader className="border-b border-border">
              <SheetTitle className="font-serif text-xl text-fg">
                Revert to checkpoint
              </SheetTitle>
              <SheetDescription className="text-fg-muted">
                Hard-resets the workdir. Newer commits become unreachable.
              </SheetDescription>
            </SheetHeader>

            <div className="space-y-4 px-4 py-6">
              <div className="rounded border border-border bg-surface-2 p-3">
                <p className="font-mono text-xs text-accent-gold">
                  {checkpoint.short_sha || checkpoint.sha.slice(0, 7)}
                </p>
                <p className="mt-1 text-sm text-fg">{checkpoint.subject}</p>
                <p className="mt-1 text-xs text-fg-dim">
                  <time dateTime={checkpoint.ts}>{relativeTime(checkpoint.ts)}</time>
                </p>
              </div>

              <PhaseBody
                phase={phase}
                onRetry={() => send(false)}
                onStashAndRevert={() => send(true)}
              />
            </div>

            <SheetFooter className="border-t border-border">
              {phase.kind === "confirm" && (
                <>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={onClose}
                    className="border-border bg-transparent text-fg hover:bg-surface-2"
                  >
                    Cancel
                  </Button>
                  <Button
                    type="button"
                    onClick={() => send(false)}
                    className="bg-accent-gold text-bg hover:opacity-90"
                  >
                    Revert
                  </Button>
                </>
              )}
              {phase.kind === "dirty" && (
                <>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={onClose}
                    className="border-border bg-transparent text-fg hover:bg-surface-2"
                  >
                    Cancel
                  </Button>
                  <Button
                    type="button"
                    onClick={() => send(true)}
                    className="bg-accent-gold text-bg hover:opacity-90"
                  >
                    Stash first then revert
                  </Button>
                </>
              )}
              {(phase.kind === "submitting" || phase.kind === "stashing") && (
                <Button
                  type="button"
                  disabled
                  className="bg-accent-gold text-bg opacity-70"
                >
                  <Loader2 size={14} className="animate-spin" aria-hidden />
                  {phase.kind === "stashing" ? "Stashing & reverting…" : "Reverting…"}
                </Button>
              )}
              {phase.kind === "network" && (
                <>
                  <Button
                    type="button"
                    variant="outline"
                    onClick={onClose}
                    className="border-border bg-transparent text-fg hover:bg-surface-2"
                  >
                    Cancel
                  </Button>
                  <Button
                    type="button"
                    onClick={() => send(false)}
                    className="bg-accent-gold text-bg hover:opacity-90"
                  >
                    Retry
                  </Button>
                </>
              )}
              {(phase.kind === "success" || phase.kind === "fatal_422") && (
                <Button
                  type="button"
                  onClick={onClose}
                  className="bg-accent-gold text-bg hover:opacity-90"
                >
                  Close
                </Button>
              )}
            </SheetFooter>
          </>
        )}
      </SheetContent>
    </Sheet>
  );
}

interface PhaseBodyProps {
  phase: Phase;
  onRetry: () => void;
  onStashAndRevert: () => void;
}

function PhaseBody({ phase }: PhaseBodyProps) {
  switch (phase.kind) {
    case "confirm":
      return (
        <p className="text-sm text-fg-muted">
          The working tree must be clean. If it isn&rsquo;t, you can stash
          and continue.
        </p>
      );
    case "submitting":
    case "stashing":
      return (
        <p className="text-sm text-fg-muted" aria-live="polite">
          {phase.kind === "stashing"
            ? "Stashing dirty files, then resetting…"
            : "Resetting working tree…"}
        </p>
      );
    case "dirty":
      return (
        <div
          className={cn(
            "rounded border-l-[3px] border-alert-ember bg-alert-ember/5 p-3",
          )}
        >
          <p className="text-xs font-semibold uppercase tracking-[0.18em] text-alert-ember">
            Working tree dirty
          </p>
          <p className="mt-1 text-sm text-fg-muted">
            {phase.dirty.length} file{phase.dirty.length === 1 ? "" : "s"} have uncommitted changes:
          </p>
          <ul
            role="list"
            className="mt-2 max-h-40 overflow-y-auto font-mono text-xs text-fg"
          >
            {phase.dirty.map((f) => (
              <li key={f} className="truncate" title={f}>
                {f}
              </li>
            ))}
          </ul>
        </div>
      );
    case "success":
      return (
        <div
          role="status"
          aria-live="polite"
          className="rounded border border-border bg-surface-2 p-3"
        >
          <p className="text-xs font-semibold uppercase tracking-[0.18em] text-live-dot">
            Reverted
          </p>
          <p className="mt-1 font-mono text-xs text-fg">
            HEAD &rarr; {phase.result.reverted_to.slice(0, 7)}
          </p>
          {phase.result.stashed_as && (
            <p className="mt-1 font-mono text-xs text-fg-dim">
              stashed: {phase.result.stashed_as}
            </p>
          )}
          <p className="mt-2 text-xs text-fg-dim">Closing…</p>
        </div>
      );
    case "fatal_422":
      return (
        <p
          role="alert"
          className="border-l-[3px] border-alert-ember pl-3 text-sm text-alert-ember"
        >
          {phase.message}
        </p>
      );
    case "network":
      return (
        <p
          role="alert"
          className="border-l-[3px] border-alert-ember pl-3 text-sm text-alert-ember"
        >
          {phase.message}
        </p>
      );
  }
}
