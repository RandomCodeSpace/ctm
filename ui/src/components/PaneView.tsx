import { useMemo } from "react";
import { usePaneStream } from "@/hooks/usePaneStream";
import { ansiToHtml } from "@/lib/ansi";
import { cn } from "@/lib/utils";

/**
 * V24 — Live tmux pane viewer. Renders the latest capture refreshed
 * at 1 Hz while mounted. Unmounting kills the SSE subscription,
 * which in turn stops the server-side capture loop (see
 * api/pane.go).
 */
export interface PaneViewProps {
  sessionName: string;
  /**
   * When false the stream is not opened. Primarily to let tests
   * mount the component in a paused state.
   */
  enabled?: boolean;
}

export function PaneView({ sessionName, enabled = true }: PaneViewProps) {
  const { text, connected, ended } = usePaneStream(sessionName, enabled);
  const html = useMemo(() => ansiToHtml(text), [text]);

  return (
    <div
      role="region"
      aria-label={`Live pane for ${sessionName}`}
      className="flex min-h-0 flex-1 flex-col"
    >
      <div className="flex shrink-0 items-center gap-2 border-b border-border bg-bg px-4 py-2">
        <span
          aria-hidden
          data-testid="pane-live-dot"
          className={cn(
            "inline-block h-2 w-2 rounded-full",
            ended
              ? "bg-fg-dim"
              : connected
                ? "bg-live-dot"
                : "bg-fg-muted",
          )}
        />
        <span className="text-[11px] font-semibold uppercase tracking-[0.18em] text-fg-muted">
          {ended ? "ended" : connected ? "live" : "connecting…"}
        </span>
      </div>
      <pre
        data-testid="pane-view"
        className={cn(
          "m-0 min-h-0 flex-1 overflow-auto whitespace-pre px-4 py-3",
          "font-mono text-[12.5px] leading-[1.45] text-fg",
        )}
        // Safe: ansiToHtml HTML-escapes all user content before
        // wrapping in <span> tags — see ui/src/lib/ansi.ts.
        dangerouslySetInnerHTML={{ __html: html || "" }}
      />
    </div>
  );
}
