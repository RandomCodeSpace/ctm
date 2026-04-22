import { useMemo } from "react";
import { useSessions } from "@/hooks/useSessions";

/**
 * Derived list of the most-recently-used unique workdirs across all
 * known sessions. Newest first. No extra network call — piggybacks on
 * the sessions cache TanStack already keeps warm.
 */
export function useRecentWorkdirs(limit = 5): string[] {
  const { data } = useSessions();
  return useMemo(() => {
    if (!data) return [];
    const seen = new Set<string>();
    const out: string[] = [];
    const sorted = data.slice().sort((a, b) => {
      const aT = Date.parse(a.last_attached_at ?? a.created_at);
      const bT = Date.parse(b.last_attached_at ?? b.created_at);
      return bT - aT;
    });
    for (const s of sorted) {
      if (!s.workdir) continue;
      if (seen.has(s.workdir)) continue;
      seen.add(s.workdir);
      out.push(s.workdir);
      if (out.length >= limit) break;
    }
    return out;
  }, [data, limit]);
}
