import { useMutation } from "@tanstack/react-query";
import { api, ApiError } from "@/lib/api";
import type { Session } from "@/hooks/useSessions";

export interface CreateSessionBody {
  workdir: string;
  name?: string;
}

export interface CreateConflict {
  error: "name_exists";
  message: string;
  session: Session;
}

/**
 * V26 create-session mutation. Server returns 201 with the new
 * Session on success; 409 with a CreateConflict body when the
 * derived name already exists (callers detect via isConflict()).
 */
export function useCreateSession() {
  return useMutation<Session, Error, CreateSessionBody>({
    mutationKey: ["create-session"],
    mutationFn: async (body) =>
      api<Session>("/api/sessions", {
        method: "POST",
        body: JSON.stringify(body),
      }),
  });
}

export function isConflict(err: unknown): err is ApiError & { body: CreateConflict } {
  return (
    err instanceof ApiError &&
    err.status === 409 &&
    typeof err.body === "object" &&
    err.body !== null &&
    (err.body as { error?: unknown }).error === "name_exists"
  );
}
