import { useState } from "react";
import { Button } from "@ossrandom/design-system";
import { ApiError } from "@/lib/api";
import { useLogin } from "@/hooks/useLogin";

interface Props {
  onSwitchToSignup?: () => void;
}

export function LoginForm({ onSwitchToSignup }: Props) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [notRegistered, setNotRegistered] = useState(false);

  const login = useLogin();

  const canSubmit =
    username.trim() !== "" && password !== "" && !login.isPending;

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setErr(null);
    setNotRegistered(false);
    try {
      await login.mutateAsync({ username, password });
    } catch (e2) {
      if (e2 instanceof ApiError && e2.status === 404) {
        setNotRegistered(true);
        setErr("No user exists on this instance yet.");
        return;
      }
      setErr(serverMessage(e2) ?? "Invalid username or password");
    }
  }

  return (
    <div className="mx-auto mt-16 w-full max-w-sm rounded-lg border border-border bg-surface p-6">
      <h1 className="mb-4 text-lg font-bold sm:text-xl">Log in to ctm</h1>
      <form onSubmit={onSubmit} className="space-y-3">
        <Field label="Email" type="email" value={username} onChange={setUsername} autoComplete="email" />
        <Field label="Password" type="password" value={password} onChange={setPassword} autoComplete="current-password" />
        {err && (
          <div role="alert" className="text-[11px] text-alert-ember">
            {err}
            {notRegistered && onSwitchToSignup && (
              <button
                type="button"
                onClick={onSwitchToSignup}
                className="ml-2 underline"
              >
                Sign up
              </button>
            )}
          </div>
        )}
        <Button
          type="submit"
          variant="danger"
          size="sm"
          block
          disabled={!canSubmit}
          loading={login.isPending}
          className="!text-[11px] !h-8 sm:!text-[13px] sm:!h-9"
        >
          Log in
        </Button>
        <p className="pt-2 text-[11px] text-fg-dim">
          Forgot password? Run <code className="rounded bg-surface-2 px-1 py-0.5 font-mono">ctm auth reset</code> on the host, then sign up again.
        </p>
      </form>
    </div>
  );
}

function Field({
  label,
  value,
  onChange,
  type = "text",
  autoComplete,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  type?: string;
  autoComplete?: string;
}) {
  return (
    <label className="block">
      <span className="mb-1 block text-[11px] font-semibold uppercase tracking-[0.18em] text-fg-muted">
        {label}
      </span>
      <input
        type={type}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        autoComplete={autoComplete}
        className="block w-full rounded border border-border bg-bg px-2 py-1.5 font-mono text-sm text-fg placeholder:text-fg-dim focus:outline-none focus:ring-1 focus:ring-accent-gold sm:text-xs"
      />
    </label>
  );
}

function serverMessage(e: unknown): string | undefined {
  if (e instanceof ApiError && typeof e.body === "object" && e.body !== null) {
    const m = (e.body as { message?: unknown }).message;
    if (typeof m === "string") return m;
  }
  return undefined;
}
