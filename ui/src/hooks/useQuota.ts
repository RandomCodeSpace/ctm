import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

export interface Quota {
  weekly_pct: number;
  five_hr_pct: number;
  weekly_resets_at: string;
  five_hr_resets_at: string;
}

/**
 * Quota cache. First paint is seeded by GET /api/quota so global
 * rate-limit bars render immediately even on a brand-new SSE
 * connection (hub.Subscribe returns no replay when Last-Event-ID is
 * empty). SseProvider then patches the same cache key in-place on
 * every `quota_update` event, so live updates keep flowing without
 * ever re-fetching.
 *
 * The server returns 204 while no statusline dump has populated rate
 * limits yet; queryFn normalizes that to null so QuotaStrip renders
 * "—" placeholders until data is known.
 */
export function useQuota() {
  return useQuery<Quota | null>({
    queryKey: ["quota"],
    queryFn: async () => {
      const res = await api<Quota | undefined>("/api/quota");
      if (!res || typeof res !== "object") return null;
      return res;
    },
    // Trust SSE for freshness once connected; REST is only the seed.
    staleTime: Infinity,
  });
}
