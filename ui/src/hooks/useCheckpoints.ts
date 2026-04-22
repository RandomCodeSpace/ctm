import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

export interface Checkpoint {
  sha: string;
  subject: string;
  author: string;
  ts: string;
  short_sha: string;
}

export interface CheckpointsResponse {
  git_workdir: boolean;
  checkpoints: Checkpoint[];
}

export function useCheckpoints(sessionName: string | undefined, limit = 50) {
  return useQuery<CheckpointsResponse>({
    queryKey: ["checkpoints", sessionName, limit],
    queryFn: () =>
      api<CheckpointsResponse>(
        `/api/sessions/${encodeURIComponent(sessionName!)}/checkpoints?limit=${limit}`,
      ),
    enabled: Boolean(sessionName),
    staleTime: 5_000,
  });
}

/**
 * V18: fetches the unified diff for a single checkpoint commit. The
 * response is text/plain (full `git show` output) — api() falls
 * through to res.text() on non-JSON content-type, so we cast the
 * generic parameter to string. Only fires when both name + sha are
 * present, matching the DiffSheet's "open only when a checkpoint is
 * selected" UX.
 *
 * Diffs are effectively immutable once committed, so a generous
 * staleTime avoids refetching every time the sheet is re-opened.
 */
export function useCheckpointDiff(
  sessionName: string | undefined,
  sha: string | undefined,
) {
  return useQuery<string>({
    queryKey: ["checkpoint-diff", sessionName, sha],
    queryFn: () =>
      api<string>(
        `/api/sessions/${encodeURIComponent(sessionName!)}/checkpoints/${encodeURIComponent(sha!)}/diff`,
      ),
    enabled: Boolean(sessionName) && Boolean(sha),
    staleTime: 60_000,
  });
}
