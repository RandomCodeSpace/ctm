import { useQuery } from "@tanstack/react-query";

export interface ToolCallRow {
  session: string;
  tool: string;
  input: string;
  summary?: string;
  is_error: boolean;
  ts: string;
  /**
   * Optional exit code from Bash tool calls. Not emitted by the Go backend
   * today — forward-compat field used by V10's Bash-only view. When absent
   * and `is_error` is false, the row is considered successful (rendered as
   * "ok"); when present or `is_error` is true, renders as "err <n>".
   */
  exit_code?: number;
  /**
   * V9 — hub Event.ID carried through SseProvider so ToolCallRow can
   * fetch full detail (diff for Edit/MultiEdit/Write) on expand.
   * Absent for REST-seeded rows from /api/feed — those can still render
   * but the expand control won't fire until a live SSE row arrives.
   */
  id?: string;
}

/**
 * Feed cache. Populated exclusively by SseProvider — on a fresh SSE
 * connect the hub replays its global ring snapshot (up to 500 events),
 * which SseProvider fans into both `["feed","all"]` and the per-session
 * cache via `appendCapped`. On reload the cache briefly shows empty
 * ("waiting…") until the SSE handshake completes (~100-300 ms), then
 * fills in one motion.
 *
 * There's a REST endpoint (GET /api/feed, GET /api/sessions/{name}/feed)
 * that mirrors the ring — kept for scripting / external consumers — but
 * we don't use it from the SPA to avoid dedup work vs. the SSE replay.
 */
export function useFeed(sessionName?: string) {
  const key: ["feed", string] = ["feed", sessionName ?? "all"];
  return useQuery<ToolCallRow[]>({
    queryKey: key,
    queryFn: () => Promise.resolve([]),
    staleTime: Infinity,
    initialData: [],
  });
}
