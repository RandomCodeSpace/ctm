import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, clearToken } from "@/lib/api";

export function useLogout() {
  const qc = useQueryClient();
  return useMutation<void, Error, void>({
    mutationKey: ["auth-logout"],
    mutationFn: async () => {
      try {
        await api<void>("/api/auth/logout", { method: "POST" });
      } catch {
        // best-effort — server may already have revoked the token
      }
    },
    onSettled: () => {
      clearToken();
      qc.clear();
    },
  });
}
