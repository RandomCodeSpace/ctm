import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import { api } from "@/lib/api";

/**
 * V15 — Subagent tree for a single session. The server returns a
 * newest-first flat list; each node carries a nullable parent_id so
 * the UI can render indentation once the backend starts resolving
 * parents (today the JSONL shape is flat, so parent_id is always
 * null and every node renders at depth 0).
 */
export interface SubagentNode {
  id: string;
  parent_id: string | null;
  type: string;
  description: string;
  started_at: string;
  stopped_at?: string | null;
  tool_calls: number;
  status: "running" | "completed" | "failed";
}

interface SubagentsResponse {
  subagents: SubagentNode[];
}

export function useSubagents(sessionName: string | undefined) {
  return useQuery<SubagentsResponse>({
    queryKey: ["subagents", sessionName],
    queryFn: () =>
      api<SubagentsResponse>(
        `/api/sessions/${encodeURIComponent(sessionName!)}/subagents`,
      ),
    enabled: Boolean(sessionName),
    staleTime: 5_000,
  });
}

/**
 * Invalidate the cached /subagents (and /teams — a subagent_start
 * implies a new team may have formed too). Used by SseProvider on
 * `subagent_start` events; kept in its own helper so future team-
 * related events can hook into the same single source of truth.
 */
export function useInvalidateSubagentsOnSse(sessionName: string | undefined) {
  // Indirection is only used by the provider; exposing this as a hook
  // keeps the hook-dependency graph explicit.
  const qc = useQueryClient();
  useEffect(() => {
    // no-op placeholder — invalidation is driven directly from
    // SseProvider by calling qc.invalidateQueries(["subagents", name]).
    // The effect just exists so the signature is a hook and can be
    // swapped for a live subscription in the future without a caller
    // change.
    return () => {
      void qc;
      void sessionName;
    };
  }, [qc, sessionName]);
}
