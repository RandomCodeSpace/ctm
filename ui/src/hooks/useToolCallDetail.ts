import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

/**
 * V9 — on-demand detail for a single tool call. Shape mirrors the Go
 * `api.Detail` struct (see internal/serve/api/tool_call_detail.go).
 *
 * diff is only populated for Edit / MultiEdit / Write; other tools
 * return an empty/missing field.
 */
export interface ToolCallDetail {
  tool: string;
  input_json: string;
  output_excerpt: string;
  ts: string;
  is_error: boolean;
  diff?: string;
}

/**
 * Fetches /api/sessions/:name/tool_calls/:id/detail on demand.
 *
 * The query is disabled by default — `ToolCallRow` flips `enabled`
 * (or calls `.refetch()`) the first time the user expands the row, so
 * we never pay for detail fetches the user never looks at. A 5 min
 * staleTime covers back-and-forth collapse/expand without refiring.
 *
 * id is the hub Event.ID format (`<unix-nano>-<seq>`). See
 * tool_call_detail.go for how the server matches that back to a
 * JSONL row.
 */
export function useToolCallDetail(
  sessionName: string | undefined,
  id: string | undefined,
  enabled: boolean,
) {
  return useQuery<ToolCallDetail>({
    queryKey: ["tool-call-detail", sessionName, id],
    queryFn: () =>
      api<ToolCallDetail>(
        `/api/sessions/${encodeURIComponent(sessionName!)}/tool_calls/${encodeURIComponent(id!)}/detail`,
      ),
    enabled: enabled && Boolean(sessionName) && Boolean(id),
    staleTime: 5 * 60_000,
  });
}
