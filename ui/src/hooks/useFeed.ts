import { useQuery } from "@tanstack/react-query";

export interface ToolCallRow {
  session: string;
  tool: string;
  input: string;
  summary?: string;
  is_error: boolean;
  ts: string;
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
