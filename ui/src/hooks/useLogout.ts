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
      // Force AuthGate to see authenticated=false immediately.
      // Don't use qc.clear() — it detaches mounted observers and the
      // subsequent re-fetch doesn't reconnect, so the UI stays stuck
      // on the authenticated tree.
      qc.setQueryData(["auth-status"], { registered: true, authenticated: false });
    },
  });
}
