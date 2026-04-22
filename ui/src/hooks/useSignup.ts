import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@/lib/api";
import { useAuth } from "@/components/AuthProvider";

export interface SignupBody { username: string; password: string }
export interface AuthSuccess { token: string; username: string }

export function useSignup() {
  const qc = useQueryClient();
  const { setTokenAndPersist } = useAuth();
  return useMutation<AuthSuccess, Error, SignupBody>({
    mutationKey: ["auth-signup"],
    mutationFn: async (body) =>
      api<AuthSuccess>("/api/auth/signup", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    onSuccess: (data) => {
      setTokenAndPersist(data.token);
      void qc.invalidateQueries({ queryKey: ["auth-status"] });
    },
  });
}
