import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

export type CostWindow = "hour" | "day" | "week";

export interface CostPoint {
  ts: string;
  session: string;
  input_tokens: number;
  output_tokens: number;
  cache_tokens: number;
  cost_usd_micros: number;
}

export interface CostTotals {
  input: number;
  output: number;
  cache: number;
  cost_usd_micros: number;
}

export interface CostResponse {
  window: CostWindow;
  points: CostPoint[];
  totals: CostTotals;
}

/**
 * V13 cumulative-cost history. 60 s staleTime — persisted cost points
 * accumulate at the statusline-dump rate (a handful per minute at
 * most), so more-aggressive refetch would just re-render the same
 * SVG. Query key includes the window and session so the Window pill
 * click in CostChart invalidates cleanly.
 */
export function useCost(sessionName?: string, window: CostWindow = "day") {
  const qs = new URLSearchParams({ window });
  if (sessionName) qs.set("session", sessionName);
  return useQuery<CostResponse>({
    queryKey: ["cost", sessionName ?? "__all__", window],
    queryFn: () => api<CostResponse>(`/api/cost?${qs.toString()}`),
    staleTime: 60_000,
    refetchInterval: 60_000,
  });
}
