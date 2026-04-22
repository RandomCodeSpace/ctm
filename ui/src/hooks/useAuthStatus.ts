import { useQuery } from "@tanstack/react-query";
import { api } from "@/lib/api";

export interface AuthStatus {
  registered: boolean;
  authenticated: boolean;
}

export function useAuthStatus() {
  return useQuery<AuthStatus, Error>({
    queryKey: ["auth-status"],
    queryFn: () => api<AuthStatus>("/api/auth/status"),
    staleTime: 0,
    refetchOnWindowFocus: false,
    retry: false,
  });
}
