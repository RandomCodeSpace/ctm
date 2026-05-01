import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
  type ReactNode,
} from "react";
import { useQueryClient } from "@tanstack/react-query";
import { useEventStream, type ParsedSseEvent } from "@/hooks/useEventStream";
import { useAuth } from "@/components/AuthProvider";
import type { Session, Attention, TokenUsage } from "@/hooks/useSessions";
import type { Quota } from "@/hooks/useQuota";
import type { ToolCallRow } from "@/hooks/useFeed";

interface SseCtx {
  connected: boolean;
}

const Ctx = createContext<SseCtx>({ connected: false });

// disconnectGrace bounds how long the SSE may flap before we consider
// the connection lost and surface the ConnectionBanner. fetch-event-
// source fires onerror on transient blips and on every reconnect cycle
// — without a debounce the banner flickers visibly on tab focus
// changes, even while data still flows.
const disconnectGrace = 3000;

const FEED_CAP = 500;

function appendCapped<T>(prev: T[] | undefined, item: T): T[] {
  const next = (prev ?? []).concat(item);
  if (next.length > FEED_CAP) next.splice(0, next.length - FEED_CAP);
  return next;
}

/**
 * Mutate a single Session in-place (returns a new array). If not found,
 * leaves the array unchanged.
 */
function patchSession(
  list: Session[] | undefined,
  name: string,
  patch: Partial<Session>,
): Session[] | undefined {
  if (!list) return list;
  const i = list.findIndex((s) => s.name === name);
  if (i < 0) return list;
  const copy = list.slice();
  copy[i] = { ...copy[i], ...patch };
  return copy;
}

/**
 * Apply a partial patch to BOTH the list cache (`['sessions']`) and the
 * single-session cache (`['sessions', name]`) so SessionDetail and
 * Dashboard see the same live updates. Without the second write the
 * detail pane silently goes stale on every SSE event.
 */
function patchSessionCaches(
  qc: ReturnType<typeof useQueryClient>,
  name: string,
  patch: Partial<Session>,
) {
  qc.setQueryData<Session[]>(["sessions"], (prev) =>
    patchSession(prev, name, patch),
  );
  qc.setQueryData<Session>(["sessions", name], (prev) =>
    prev ? { ...prev, ...patch } : prev,
  );
}

/**
 * Single SSE subscription to /events/all. Mutates QueryClient cache so any
 * component reading the same keys (sessions, feed, quota, attention) sees
 * live updates without their own subscription.
 *
 * Cache mutation map (spec §5):
 *   session_new / session_attached → upsert into ['sessions']
 *   session_killed                 → drop from ['sessions']
 *   tool_call                      → push to ['feed', name] AND ['feed', 'all']
 *   quota_update (no session)      → setQueryData(['quota'])
 *   quota_update (with session)    → patch context_pct on the session row
 *   attention_raised/_cleared      → setQueryData(['attention', name])
 *                                    AND patch attention on the session row
 */
export function SseProvider({ children }: { children: ReactNode }) {
  const queryClient = useQueryClient();
  const { signOut, token } = useAuth();
  const [connected, setConnected] = useState(false);

  const handle = useCallback(
    (ev: ParsedSseEvent) => {
      const data = ev.data as Record<string, unknown>;

      switch (ev.type) {
        case "session_new":
        case "session_attached": {
          const s = data as unknown as Session;
          queryClient.setQueryData<Session[]>(["sessions"], (prev) => {
            if (!prev) return [s];
            const i = prev.findIndex((x) => x.name === s.name);
            if (i >= 0) {
              const copy = prev.slice();
              copy[i] = { ...copy[i], ...s };
              return copy;
            }
            return prev.concat(s);
          });
          // Mirror the upsert into the per-session cache so SessionDetail
          // doesn't go stale.
          queryClient.setQueryData<Session>(["sessions", s.name], (prev) =>
            prev ? { ...prev, ...s } : s,
          );
          break;
        }
        case "session_killed": {
          const name = (data as { name: string }).name;
          queryClient.setQueryData<Session[]>(["sessions"], (prev) =>
            (prev ?? []).filter((x) => x.name !== name),
          );
          queryClient.removeQueries({ queryKey: ["sessions", name] });
          break;
        }
        case "tool_call": {
          // Stamp the hub Event.ID onto the row so ToolCallRow (V9)
          // can fetch detail on expand. The payload itself doesn't
          // carry its own id — SSE envelopes it separately.
          const row = { ...(data as unknown as ToolCallRow), id: ev.id } as ToolCallRow;
          queryClient.setQueryData<ToolCallRow[]>(
            ["feed", "all"],
            (prev) => appendCapped(prev, row),
          );
          if (row.session) {
            queryClient.setQueryData<ToolCallRow[]>(
              ["feed", row.session],
              (prev) => appendCapped(prev, row),
            );
            // Best-effort: bump last_tool_call_at on the session row so the
            // HealthDot flips to "live" without a refetch.
            patchSessionCaches(queryClient, row.session, {
              last_tool_call_at: row.ts,
            });
          }
          break;
        }
        case "quota_update": {
          // Spec §5: a quota_update may carry a `session` field, in which
          // case it's a per-session push (context_pct + live token
          // breakdown) rather than a global rate-limit quota.
          const sessionName = (data as { session?: string }).session;
          if (sessionName) {
            const patch: Partial<Session> = {};
            const d = data as {
              context_pct?: number;
              input_tokens?: number;
              output_tokens?: number;
              cache_tokens?: number;
            };
            if (typeof d.context_pct === "number") patch.context_pct = d.context_pct;
            if (
              typeof d.input_tokens === "number" ||
              typeof d.output_tokens === "number" ||
              typeof d.cache_tokens === "number"
            ) {
              patch.tokens = {
                input_tokens: d.input_tokens ?? 0,
                output_tokens: d.output_tokens ?? 0,
                cache_tokens: d.cache_tokens ?? 0,
              } satisfies TokenUsage;
            }
            if (Object.keys(patch).length > 0) {
              patchSessionCaches(queryClient, sessionName, patch);
            }
            break;
          }
          queryClient.setQueryData<Quota>(["quota"], data as unknown as Quota);
          break;
        }
        case "attention_raised": {
          const { session, ...rest } = data as unknown as {
            session: string;
          } & Attention;
          queryClient.setQueryData<Attention>(["attention", session], rest);
          // Mirror into both session caches so the sort + halftone
          // treatment refresh without a refetch in either pane.
          patchSessionCaches(queryClient, session, { attention: rest });
          break;
        }
        case "attention_cleared": {
          const session = (data as { session: string }).session;
          queryClient.setQueryData<Attention>(["attention", session], {
            state: "clear",
          });
          patchSessionCaches(queryClient, session, {
            attention: { state: "clear" },
          });
          break;
        }
        // V15 — subagent lifecycle. The tailer emits `subagent_start`
        // when a new agent_id first appears in the session's JSONL;
        // we invalidate both the subagent forest and the derived
        // teams list since a new subagent may reshape the dispatch-
        // window clusters.
        case "subagent_start":
        case "subagent_stop": {
          const session = (data as { session?: string }).session;
          if (session) {
            queryClient.invalidateQueries({ queryKey: ["subagents", session] });
            queryClient.invalidateQueries({ queryKey: ["teams", session] });
          }
          break;
        }
        // V16 — team lifecycle. If/when the backend emits explicit
        // team_spawn / team_settled events, the UI just refetches.
        case "team_spawn":
        case "team_settled": {
          const session = (data as { session?: string }).session;
          if (session) {
            queryClient.invalidateQueries({ queryKey: ["teams", session] });
          }
          break;
        }
      }
    },
    [queryClient],
  );

  // Debounce the "lost" transition so onerror blips during a normal
  // reconnect cycle (or on tab focus changes) don't flap the banner.
  // The "connected" transition is immediate — recovery should feel
  // instant.
  const lostTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const markConnected = useCallback(() => {
    if (lostTimerRef.current !== null) {
      globalThis.clearTimeout(lostTimerRef.current);
      lostTimerRef.current = null;
    }
    setConnected(true);
  }, []);
  const markDisconnected = useCallback(() => {
    if (lostTimerRef.current !== null) return;
    lostTimerRef.current = globalThis.setTimeout(() => {
      lostTimerRef.current = null;
      setConnected(false);
    }, disconnectGrace);
  }, []);
  useEffect(
    () => () => {
      if (lostTimerRef.current !== null) {
        globalThis.clearTimeout(lostTimerRef.current);
        lostTimerRef.current = null;
      }
    },
    [],
  );

  useEventStream({
    url: "/events/all",
    enabled: Boolean(token),
    key: token ?? "",
    onEvent: handle,
    onOpen: markConnected,
    onError: markDisconnected,
    onUnauthorized: () => signOut(),
  });

  const value = useMemo(() => ({ connected }), [connected]);
  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useSseStatus() {
  return useContext(Ctx);
}
