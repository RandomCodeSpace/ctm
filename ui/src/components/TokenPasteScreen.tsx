import { useState, type FormEvent } from "react";
import { api, UnauthorizedError } from "@/lib/api";
import { useAuth } from "@/components/AuthProvider";

interface BootstrapResponse {
  version: string;
  port: number;
  has_webhook: boolean;
}

/**
 * First-load auth screen. User pastes contents of ~/.config/ctm/serve.token.
 * On submit: GET /api/bootstrap with bearer. 200 → persist + navigate.
 * 401 → inline error.
 */
export function TokenPasteScreen() {
  const { setTokenAndPersist } = useAuth();
  const [value, setValue] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    const trimmed = value.trim();
    if (!trimmed) return;
    setBusy(true);
    setError(null);
    try {
      await api<BootstrapResponse>("/api/bootstrap", { token: trimmed });
      setTokenAndPersist(trimmed);
      const params = new URLSearchParams(window.location.search);
      let next = params.get("next") || "/";
      // Reject anything but a same-origin relative path. Blocks
      // javascript:/data: URI execution and protocol-relative
      // (//evil.example) open-redirect via a crafted ?next= link.
      if (!next.startsWith("/") || next.startsWith("//")) {
        next = "/";
      }
      window.location.replace(next);
    } catch (err) {
      if (err instanceof UnauthorizedError) {
        setError("Invalid token. Re-copy from ~/.config/ctm/serve.token.");
      } else {
        setError(err instanceof Error ? err.message : "Unknown error");
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="flex min-h-screen items-center justify-center bg-bg px-6">
      <form
        onSubmit={onSubmit}
        className="w-full max-w-md space-y-6 rounded border border-border bg-surface p-8"
      >
        <header className="space-y-2">
          <h1 className="font-serif text-2xl font-bold text-fg">ctm</h1>
          <p className="text-sm text-fg-muted">
            Paste the contents of{" "}
            <code className="font-mono text-fg">~/.config/ctm/serve.token</code>
            {" "}to continue.
          </p>
        </header>

        <label className="block space-y-2">
          <span className="text-xs font-medium uppercase tracking-[0.18em] text-fg-muted">
            Bearer token
          </span>
          <textarea
            autoFocus
            value={value}
            onChange={(e) => setValue(e.target.value)}
            rows={4}
            spellCheck={false}
            className="w-full resize-none rounded border border-border bg-surface-2 px-3 py-2 font-mono text-xs text-fg focus:border-accent-gold focus:outline-none"
            placeholder="paste here…"
            disabled={busy}
          />
        </label>

        {error && (
          <p
            role="alert"
            className="border-l-2 border-alert-ember pl-3 text-sm text-alert-ember"
          >
            {error}
          </p>
        )}

        <button
          type="submit"
          disabled={busy || !value.trim()}
          className="w-full rounded bg-accent-gold px-4 py-2 text-sm font-medium uppercase tracking-[0.18em] text-bg transition-opacity hover:opacity-90 disabled:opacity-40"
        >
          {busy ? "Verifying…" : "Continue"}
        </button>
      </form>
    </main>
  );
}
