import { ApiError } from "@/lib/api";

// Pulls a `message` field out of an ApiError body when the server sent
// one. LoginForm and SignupForm both want this — `await mutateAsync` can
// surface a typed ApiError whose body shape is `{ message?: string }`.
export function serverMessage(e: unknown): string | undefined {
  if (e instanceof ApiError && typeof e.body === "object" && e.body !== null) {
    const m = (e.body as { message?: unknown }).message;
    if (typeof m === "string") return m;
  }
  return undefined;
}
