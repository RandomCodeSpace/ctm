import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { useAuth } from "@/components/AuthProvider";

export interface LoginBody { username: string; password: string }
export interface AuthSuccess { token: string; username: string }

export function useLogin() {
  const qc = useQueryClient();
  const { setTokenAndPersist } = useAuth();
  return useMutation<AuthSuccess, Error, LoginBody>({
    mutationKey: ["auth-login"],
    mutationFn: async (body) =>
      api<AuthSuccess>("/api/auth/login", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    onSuccess: (data) => {
      setTokenAndPersist(data.token);
      void qc.invalidateQueries({ queryKey: ["auth-status"] });
    },
  });
}
