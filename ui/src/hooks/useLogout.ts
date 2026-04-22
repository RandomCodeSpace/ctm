import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { useAuth } from "@/components/AuthProvider";

export function useLogout() {
  const qc = useQueryClient();
  const { signOut } = useAuth();
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
      signOut();
      qc.clear();
    },
  });
}
