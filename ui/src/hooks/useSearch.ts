import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

/**
 * V19 search response — FTS5 index query. Wire shape mirrors
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
  truncated: boolean;
}

const MIN_Q = 3;

/**
 * Palette search hook. Gated on q.length >= 3 (matches the FTS5 trigram
 * tokenizer's minimum useful query length and the handler's 3..256
 * range). `keepPreviousData` keeps the old list visible while the next
 * query flies so the palette doesn't flash empty between keystrokes.
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
