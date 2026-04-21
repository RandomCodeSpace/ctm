import { api } from "@/lib/api";
import type { ToolCallRow } from "@/hooks/useFeed";

/**
 * V6 — historical scroll past the in-memory 500-slot ring buffer.
 *
 * This is a mutation-style fetcher, NOT an auto-fetching TanStack query.
 * Rationale: the feed cache (["feed", name]) is a live, append-only
 * mirror of the hub ring. If we merged historical events into that
 * cache, every SSE tick would need to re-sort / de-dup against older
 * rows — costly and error-prone. Instead, the caller owns the merged
 * list (ring snapshot + historical append) and renders it explicitly.
 *
 * Backend: GET /api/sessions/{name}/feed/history?before=<id>&limit=N.
 * `before` is the oldest currently-visible event's id (cursor);
 * response is {events, has_more} newest-first.
 */

/** One row returned by /feed/history. */
export interface FeedHistoryEvent {
  id: string;
  session: string;
  type: string;
  ts: string;
  payload: ToolCallRow;
}

export interface FeedHistoryResponse {
  events: FeedHistoryEvent[];
  has_more: boolean;
}

/**
 * Fetch older tool_call events for `sessionName` strictly before
 * `beforeId`. Returns the raw response so the caller can append the
 * payloads below the ring view AND track has_more for the button
 * visibility.
 */
export async function fetchFeedHistory(
  sessionName: string,
  beforeId: string,
  limit = 100,
): Promise<FeedHistoryResponse> {
  const qs = new URLSearchParams({
    before: beforeId,
    limit: String(limit),
  });
  return api<FeedHistoryResponse>(
    `/api/sessions/${encodeURIComponent(sessionName)}/feed/history?${qs}`,
  );
}
