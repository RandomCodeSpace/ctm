import { useEffect, useRef, useState } from "react";
import {
  fetchEventSource,
  type EventSourceMessage,
} from "@microsoft/fetch-event-source";
import { authHeaders, UnauthorizedError } from "@/lib/api";

/**
 * V24 — subscribe to `/events/session/{name}/pane` for the lifetime of
 * the hook. Returns the most recent capture text.
 *
 * The SSE body encodes `data:` as a JSON string (produced by the Go
 * `json.Marshal` on the server side); we parse it back to the raw
 * capture (with escape sequences) so `ansiToHtml` can colourise it.
 *
 * Unmounting aborts the fetch — which causes the server handler to
 * exit at the next context check and stop shelling out to tmux. This
 * is the mechanism the design relies on for "server stops when client
 * disconnects".
 */
export interface UsePaneStreamResult {
  text: string;
  connected: boolean;
  ended: boolean;
}

export function usePaneStream(
  sessionName: string | undefined,
  enabled: boolean,
): UsePaneStreamResult {
  const [text, setText] = useState<string>("");
  const [connected, setConnected] = useState<boolean>(false);
  const [ended, setEnded] = useState<boolean>(false);
  const ctrlRef = useRef<AbortController | null>(null);

  useEffect(() => {
    if (!enabled || !sessionName) return;
    const ctrl = new AbortController();
    ctrlRef.current = ctrl;
    setConnected(false);
    setEnded(false);

    fetchEventSource(
      `/events/session/${encodeURIComponent(sessionName)}/pane`,
      {
        signal: ctrl.signal,
        headers: { ...authHeaders(), Accept: "text/event-stream" },
        openWhenHidden: true,
        async onopen(res) {
          if (res.status === 401) {
            throw new UnauthorizedError(`401 on pane stream`);
          }
          if (!res.ok) {
            throw new Error(`pane SSE open failed ${res.status}`);
          }
          setConnected(true);
        },
        onmessage(ev: EventSourceMessage) {
          if (ev.event === "pane") {
            try {
              const parsed = JSON.parse(ev.data);
              if (typeof parsed === "string") {
                setText(parsed);
              }
            } catch {
              // Fall back to raw — keeps the viewer alive even if a
              // proxy rewrites the payload.
              setText(ev.data);
            }
          } else if (ev.event === "pane_end") {
            setEnded(true);
          }
        },
        onerror(err) {
          setConnected(false);
          if (err instanceof UnauthorizedError) throw err;
          // Let fetch-event-source retry.
        },
      },
    ).catch(() => {
      setConnected(false);
    });

    return () => {
      ctrl.abort();
      ctrlRef.current = null;
    };
  }, [sessionName, enabled]);

  return { text, connected, ended };
}
