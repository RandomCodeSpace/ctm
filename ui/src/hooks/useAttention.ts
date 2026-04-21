import { useQuery } from "@tanstack/react-query";
import type { Attention } from "@/hooks/useSessions";

/**
 * Per-session attention cache. Populated by SseProvider on
 * `attention_raised` / `attention_cleared` events. There's no REST
 * endpoint in v0.1; the live cache is the source of truth.
 */
export function useAttention(sessionName: string | undefined) {
  const enabled = Boolean(sessionName);
  return useQuery<Attention>({
    queryKey: ["attention", sessionName],
    queryFn: () => Promise.resolve({ state: "clear" } as Attention),
    enabled,
    staleTime: Infinity,
    initialData: { state: "clear" } as Attention,
  });
}
