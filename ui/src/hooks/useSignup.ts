import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api, setToken } from "@/lib/api";

export interface SignupBody { username: string; password: string }
export interface AuthSuccess { token: string; username: string }

export function useSignup() {
  const qc = useQueryClient();
  return useMutation<AuthSuccess, Error, SignupBody>({
    mutationKey: ["auth-signup"],
    mutationFn: async (body) =>
      api<AuthSuccess>("/api/auth/signup", {
        method: "POST",
        body: JSON.stringify(body),
      }),
    onSuccess: (data) => {
      setToken(data.token);
      void qc.invalidateQueries({ queryKey: ["auth-status"] });
    },
  });
}
