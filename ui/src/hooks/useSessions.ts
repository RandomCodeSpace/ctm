import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

export interface Attention {
  state:
    | "clear"
    | "error_burst"
    | "stalled"
    | "quota_low"
    | "permission_request"
    | "context_high"
    | "long_session"
    | "tmux_dead"
    | string;
  since?: string;
  details?: string;
}

export interface TokenUsage {
  input_tokens: number;
  output_tokens: number;
  /** Sum of cache_creation_input_tokens + cache_read_input_tokens. */
  cache_tokens: number;
}

export interface Session {
  name: string;
  uuid: string;
  mode: "yolo" | "safe";
  workdir: string;
  created_at: string;
  last_attached_at?: string;
  is_active: boolean;
  tmux_alive: boolean;
  last_tool_call_at?: string;
  context_pct?: number;
  tokens?: TokenUsage;
  attention?: Attention;
}

export function useSessions() {
  return useQuery({
    queryKey: ["sessions"],
    queryFn: () => api<Session[]>("/api/sessions"),
  });
}

export function useSession(name: string | undefined) {
  return useQuery({
    queryKey: ["sessions", name],
    queryFn: () => api<Session>(`/api/sessions/${encodeURIComponent(name!)}`),
    enabled: Boolean(name),
  });
}

/**
 * Severity ordering for sort. Higher = more urgent. Spec §5: list is sorted
 * by attention.severity DESC, last_attached_at DESC.
 */
const SEVERITY: Record<string, number> = {
  permission_request: 60,
  error_burst: 50,
  tmux_dead: 40,
  quota_low: 30,
  stalled: 20,
  context_high: 15,
  long_session: 10,
  clear: 0,
};

export function attentionSeverity(a: Attention | undefined): number {
  if (!a || a.state === "clear") return 0;
  return SEVERITY[a.state] ?? 5;
}

/** Stable comparator for the active session list. */
export function sortSessions(a: Session, b: Session): number {
  const sb = attentionSeverity(b.attention) - attentionSeverity(a.attention);
  if (sb !== 0) return sb;
  const ta = a.last_attached_at ? Date.parse(a.last_attached_at) : 0;
  const tb = b.last_attached_at ? Date.parse(b.last_attached_at) : 0;
  return tb - ta;
}
