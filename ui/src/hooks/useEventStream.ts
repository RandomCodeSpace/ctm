import { useEffect, useRef } from "react";
import {
  fetchEventSource,
  type EventSourceMessage,
} from "@microsoft/fetch-event-source";
import { authHeaders, UnauthorizedError } from "@/lib/api";

export type SseEventType =
  | "tool_call"
  | "quota_update"
  | "session_new"
  | "session_killed"
  | "session_attached"
  | "attention_raised"
  | "attention_cleared";

export interface ParsedSseEvent {
  id: string;
  type: SseEventType | string;
  data: unknown;
}

export interface UseEventStreamOpts {
  /** Path: "/events/all" or "/events/session/<name>". */
  url: string;
  enabled?: boolean;
  /**
   * Additional cache-busting key — when this changes, the hook aborts the
   * current subscription and opens a new one. Used by SseProvider to pass
   * the bearer token so rotation triggers a clean re-subscribe (the bearer
   * is baked into the initial fetch request; auto-reconnect would keep
   * using the stale token otherwise).
   */
  key?: string;
  onEvent: (event: ParsedSseEvent) => void;
  onOpen?: () => void;
  onError?: (err: unknown) => void;
  onUnauthorized?: () => void;
}

/**
 * Subscribe to an SSE stream with bearer auth. Auto-reconnects (handled by
 * fetch-event-source); replays from `Last-Event-ID` server-side.
 */
export function useEventStream({
  url,
  enabled = true,
  key,
  onEvent,
  onOpen,
  onError,
  onUnauthorized,
}: UseEventStreamOpts) {
  // Stash callbacks in refs so the effect doesn't re-subscribe on every render.
  const onEventRef = useRef(onEvent);
  const onOpenRef = useRef(onOpen);
  const onErrorRef = useRef(onError);
  const onUnauthorizedRef = useRef(onUnauthorized);
  onEventRef.current = onEvent;
  onOpenRef.current = onOpen;
  onErrorRef.current = onError;
  onUnauthorizedRef.current = onUnauthorized;

  useEffect(() => {
    if (!enabled) return;
    const ctrl = new AbortController();

    fetchEventSource(url, {
      signal: ctrl.signal,
      headers: { ...authHeaders(), Accept: "text/event-stream" },
      openWhenHidden: true,
      async onopen(res) {
        if (res.status === 401) {
          onUnauthorizedRef.current?.();
          throw new UnauthorizedError(`401 on ${url}`);
        }
        if (!res.ok) {
          throw new Error(`SSE open failed ${res.status} ${res.statusText}`);
        }
        onOpenRef.current?.();
      },
      onmessage(ev: EventSourceMessage) {
        let data: unknown = ev.data;
        try {
          data = JSON.parse(ev.data);
        } catch {
          /* keep raw */
        }
        onEventRef.current({
          id: ev.id ?? "",
          type: ev.event || "message",
          data,
        });
      },
      onerror(err) {
        onErrorRef.current?.(err);
        if (err instanceof UnauthorizedError) {
          // Stop retrying — auth handler is in charge.
          throw err;
        }
        // Returning undefined → fetch-event-source will retry.
      },
    }).catch((err) => {
      // Final failure (after onerror threw). Surface once.
      onErrorRef.current?.(err);
    });

    return () => ctrl.abort();
  }, [url, enabled, key]);
}
