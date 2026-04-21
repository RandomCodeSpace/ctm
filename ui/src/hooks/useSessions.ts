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
  last_tool_call_at?: string;
  is_active: boolean;
  tmux_alive: boolean;
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
 * Severity buckets from the v0.1 spec. Retained as an export so the
 * SessionCard AttentionLabel can colour-code by urgency, but no longer
 * the primary sort key — users found "stuck" sessions jumping to the
 * top disorienting. The list sort is now age-only on most-recent
 * activity (see sortSessions).
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

/** A session is "stale" when it's been alive but inactive for longer
 * than this. Frontend-only constant; if we later want per-install
 * tuning, thread through /api/bootstrap. */
export const STALE_THRESHOLD_MS = 30 * 60 * 1000;

/** True when the session's tmux is alive but no tool call has landed
 * within STALE_THRESHOLD_MS. Used by the StaleChip on SessionCard. */
export function isStale(s: Session, nowMs: number = Date.now()): boolean {
  if (!s.tmux_alive) return false;
  if (!s.last_tool_call_at) return false;
  return nowMs - Date.parse(s.last_tool_call_at) > STALE_THRESHOLD_MS;
}

/** Most-recent activity timestamp for sort: prefer last tool call, fall
 * back to last_attached_at, then created_at. */
function activityMs(s: Session): number {
  if (s.last_tool_call_at) return Date.parse(s.last_tool_call_at);
  if (s.last_attached_at) return Date.parse(s.last_attached_at);
  return s.created_at ? Date.parse(s.created_at) : 0;
}

/** Age-only comparator: newest tool-call activity first. */
export function sortSessions(a: Session, b: Session): number {
  return activityMs(b) - activityMs(a);
}
