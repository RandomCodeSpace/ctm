import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

/**
 * V19 Slice 1 grep-style search response. Wire shape mirrors
 * internal/serve/api/search.go:SearchResponse exactly.
 */
export interface SearchMatch {
  session: string;
  uuid: string;
  ts?: string;
  tool?: string;
  snippet: string;
}

export interface SearchResponse {
  query: string;
  matches: SearchMatch[];
  scanned_files: number;
  truncated: boolean;
}

const MIN_Q = 2;

/**
 * Palette search hook. Gated on q.length >= 2 (mirrors the handler's
 * own 2..256 range so we don't issue guaranteed-400s). `keepPreviousData`
 * keeps the old result list visible while the next query flies so the
 * palette doesn't flash empty between keystrokes.
 */
export function useSearch(q: string, sessionName?: string) {
  const trimmed = q.trim();
  const enabled = trimmed.length >= MIN_Q;
  return useQuery<SearchResponse>({
    queryKey: ["search", trimmed, sessionName ?? ""],
    queryFn: () => {
      const params = new URLSearchParams({ q: trimmed });
      if (sessionName) params.set("session", sessionName);
      return api<SearchResponse>(`/api/search?${params.toString()}`);
    },
    enabled,
    placeholderData: keepPreviousData,
    // Ephemeral results — 30s is a sane ceiling for a palette.
    staleTime: 30_000,
  });
}
