import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";

/**
 * V0.2 Settings drawer contract. Both shapes mirror the Go
 * `configPayload` struct in internal/serve/api/config_get.go — any
 * schema change must be made in lockstep with that handler.
 *
 * `webhook_auth` is the literal Authorization header the daemon sends
 * on POSTed webhook events. Empty string disables the header.
 */
export interface AttentionThresholds {
  error_rate_pct: number;
  error_rate_window: number;
  idle_minutes: number;
  quota_pct: number;
  context_pct: number;
  yolo_unchecked_minutes: number;
}

export interface ConfigPayload {
  webhook_url: string;
  webhook_auth: string;
  attention: AttentionThresholds;
}

/**
 * GET /api/config. The daemon always returns resolved defaults for
 * any zero-valued attention thresholds, so the form never starts with
 * a confusing 0 in a number input.
 *
 * `staleTime: 0` because the drawer is rarely opened — when it is, we
 * want the latest on-disk state, not a cached snapshot from a previous
 * open.
 */
export function useConfigGet(enabled: boolean) {
  return useQuery<ConfigPayload>({
    queryKey: ["config"],
    queryFn: () => api<ConfigPayload>("/api/config"),
    enabled,
    staleTime: 0,
  });
}

/**
 * PATCH /api/config. Server returns 202 + {status:"restarting"} and
 * schedules a graceful shutdown 1s later; the ConnectionBanner takes
 * over from there via SSE reconnect.
 *
 * On success we invalidate the config query so if the user re-opens
 * the drawer post-reconnect they see their saved values.
 */
export function useConfigUpdate() {
  const qc = useQueryClient();
  return useMutation<{ status: string }, Error, ConfigPayload>({
    mutationFn: (body) =>
      api<{ status: string }>("/api/config", {
        method: "PATCH",
        body: JSON.stringify(body),
      }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["config"] });
    },
  });
}
