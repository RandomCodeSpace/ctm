import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

export interface TeamMember {
  subagent_id: string;
  description: string;
  status: "running" | "completed" | "failed";
}

export interface Team {
  id: string;
  name: string;
  dispatched_at: string;
  status: "running" | "completed" | "failed";
  summary?: string | null;
  members: TeamMember[];
}

interface TeamsResponse {
  teams: Team[];
}

/**
 * V16 — "Agent teams" grouped by dispatch-window heuristic (see
 * internal/serve/api/teams.go). Today the server infers teams from
 * clustered start times; the client doesn't need to care.
 */
export function useTeams(sessionName: string | undefined) {
  return useQuery<TeamsResponse>({
    queryKey: ["teams", sessionName],
    queryFn: () =>
      api<TeamsResponse>(
        `/api/sessions/${encodeURIComponent(sessionName!)}/teams`,
      ),
    enabled: Boolean(sessionName),
    staleTime: 5_000,
  });
}
