import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

export interface Checkpoint {
  sha: string;
  subject: string;
  author: string;
  ts: string;
  short_sha: string;
}

export function useCheckpoints(sessionName: string | undefined, limit = 50) {
  return useQuery<Checkpoint[]>({
    queryKey: ["checkpoints", sessionName, limit],
    queryFn: () =>
      api<Checkpoint[]>(
        `/api/sessions/${encodeURIComponent(sessionName!)}/checkpoints?limit=${limit}`,
      ),
    enabled: Boolean(sessionName),
    staleTime: 5_000,
  });
}
