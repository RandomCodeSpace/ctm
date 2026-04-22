import { useMutation } from "@tanstack/react-query";
import { api } from "@/lib/api";

/**
 * V25 session-input mutation. Body is exactly one of:
 *   { text: string }  — 1..256 chars, no newlines or control chars
 *   { preset: "yes" | "no" | "continue" }
 *
 * Server returns 204 No Content on success; on error the response
 * body is { error: <code>, message: <text> }. Callers surface the
 * message inline under the input bar.
 */
export type SendInputBody =
  | { text: string; preset?: never }
  | { preset: "yes" | "no" | "continue" | "follow"; text?: never };

export function useSendInput(sessionName: string | undefined) {
  return useMutation<void, Error, SendInputBody>({
    mutationKey: ["send-input", sessionName],
    mutationFn: async (body) => {
      if (!sessionName) throw new Error("missing session name");
      await api<void>(
        `/api/sessions/${encodeURIComponent(sessionName)}/input`,
        {
          method: "POST",
          body: JSON.stringify(body),
        },
      );
    },
  });
}
