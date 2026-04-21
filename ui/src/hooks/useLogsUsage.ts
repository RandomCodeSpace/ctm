import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

export interface LogsUsageFile {
  uuid: string;
  session: string;
  bytes: number;
  mtime: string;
}

export interface LogsUsage {
  dir: string;
  total_bytes: number;
  files: LogsUsageFile[];
}

/**
 * V21 log disk usage. 30 s staleTime — disk usage moves slowly and the
 * walk is bounded server-side, so TanStack's default aggressive refetch
 * would burn FDs without changing the pixels.
 */
export function useLogsUsage() {
  return useQuery<LogsUsage>({
    queryKey: ["logs-usage"],
    queryFn: () => api<LogsUsage>("/api/logs/usage"),
    staleTime: 30_000,
  });
}
