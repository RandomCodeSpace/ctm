import { useCallback, useEffect, useRef, useState } from "react";
// Note: using useRef only for the error timer; the text input is
// uncontrolled-from-React's-perspective via its `value` prop.
import { Send } from "lucide-react";
import { ApiError } from "@/lib/api";
import { useSendInput } from "@/hooks/useSendInput";
import { cn } from "@/lib/utils";

interface SessionInputBarProps {
  sessionName: string;
  mode: "safe" | "yolo";
}

type Preset = "yes" | "no" | "continue";

/**
 * V25 — sticky bottom bar that types short answers into a running
 * yolo-mode claude via POST /api/sessions/<name>/input. Rendered
 * only when the current session is yolo; for safe sessions the bar
 * is intentionally absent.
 */
export function SessionInputBar({ sessionName, mode }: SessionInputBarProps) {
  const send = useSendInput(sessionName);
  const [text, setText] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const errTimer = useRef<number | null>(null);

  const flash = useCallback((msg: string) => {
    setErr(msg);
    if (errTimer.current != null) {
      window.clearTimeout(errTimer.current);
    }
    errTimer.current = window.setTimeout(() => setErr(null), 3000);
  }, []);

  useEffect(() => {
    return () => {
      if (errTimer.current != null) window.clearTimeout(errTimer.current);
    };
  }, []);

  const runPreset = useCallback(
    (preset: Preset) => {
      send.mutate(
        { preset },
        {
          onError: (e) => flash(serverMessage(e) ?? "Could not send input"),
        },
      );
    },
    [send, flash],
  );

  const runText = useCallback(() => {
    const trimmed = text.trim();
    if (!trimmed) return;
    send.mutate(
      { text: trimmed },
      {
        onSuccess: () => setText(""),
        onError: (e) => flash(serverMessage(e) ?? "Could not send input"),
      },
    );
  }, [text, send, flash]);

  if (mode !== "yolo") return null;

  return (
    <div
      aria-label="Send input to claude"
      className={cn(
        "flex shrink-0 flex-col gap-1 border-t border-border bg-bg px-3 py-2",
      )}
    >
      <div className="flex items-center gap-1.5">
        <PresetButton label="Approve" onClick={() => runPreset("yes")} />
        <PresetButton label="Deny" onClick={() => runPreset("no")} />
        <PresetButton label="Continue" onClick={() => runPreset("continue")} />

        <form
          className="ml-auto flex min-w-0 flex-1 items-center gap-1.5"
          onSubmit={(e) => {
            e.preventDefault();
            runText();
          }}
        >
          <input
            type="text"
            maxLength={256}
            aria-label="Custom input"
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder="Type a reply…"
            className="min-w-0 flex-1 rounded border border-border bg-surface px-2 py-1 font-mono text-xs text-fg placeholder:text-fg-dim focus:outline-none focus:ring-1 focus:ring-accent-gold"
          />
          <button
            type="submit"
            aria-label="Send"
            title="Send"
            disabled={text.trim() === "" || send.isPending}
            className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded border border-border bg-surface text-fg-muted transition-colors hover:bg-surface-2 hover:text-fg disabled:cursor-not-allowed disabled:opacity-40"
          >
            <Send size={14} aria-hidden />
          </button>
        </form>
      </div>

      {err && (
        <div
          role="status"
          className="px-1 text-[11px] text-alert-ember"
        >
          {err}
        </div>
      )}
    </div>
  );
}

function PresetButton({
  label,
  onClick,
}: {
  label: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      aria-label={label}
      onClick={onClick}
      className="shrink-0 rounded border border-border bg-surface px-2 py-1 text-[10px] font-semibold uppercase tracking-[0.14em] text-fg transition-colors hover:bg-surface-2"
    >
      {label}
    </button>
  );
}

function serverMessage(e: unknown): string | undefined {
  if (e instanceof ApiError && typeof e.body === "object" && e.body !== null) {
    const m = (e.body as { message?: unknown }).message;
    if (typeof m === "string") return m;
  }
  return undefined;
}
