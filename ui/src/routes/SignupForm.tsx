import { useState } from "react";
import { ApiError } from "@/lib/api";
import { useSignup } from "@/hooks/useSignup";

interface Props {
  onSwitchToLogin?: () => void;
}

export function SignupForm({ onSwitchToLogin }: Props) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [isAlreadyRegistered, setAlready] = useState(false);

  const signup = useSignup();

  const canSubmit =
    username.trim() !== "" &&
    password !== "" &&
    confirm !== "" &&
    password === confirm &&
    !signup.isPending;

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setErr(null);
    setAlready(false);
    try {
      await signup.mutateAsync({ username, password });
    } catch (e2) {
      if (e2 instanceof ApiError && e2.status === 409) {
        setAlready(true);
        setErr("This instance already has a user. Log in with those credentials.");
        return;
      }
      setErr(serverMessage(e2) ?? "Could not sign up");
    }
  }

  return (
    <div className="mx-auto mt-16 w-full max-w-sm rounded-lg border border-border bg-surface p-6">
      <h1 className="mb-4 font-serif text-xl font-bold">Create your ctm account</h1>
      <form onSubmit={onSubmit} className="space-y-3">
        <Field label="Username" value={username} onChange={setUsername} autoComplete="username" />
        <Field label="Password" type="password" value={password} onChange={setPassword} autoComplete="new-password" />
        <Field label="Confirm password" type="password" value={confirm} onChange={setConfirm} autoComplete="new-password" />
        {err && (
          <div role="alert" className="text-[11px] text-alert-ember">
            {err}
            {isAlreadyRegistered && onSwitchToLogin && (
              <button
                type="button"
                onClick={onSwitchToLogin}
                className="ml-2 underline"
              >
                Log in instead
              </button>
            )}
          </div>
        )}
        <button
          type="submit"
          disabled={!canSubmit}
          className="w-full rounded border border-border bg-accent-gold px-3 py-2 text-xs font-semibold text-bg hover:brightness-110 disabled:cursor-not-allowed disabled:opacity-40"
        >
          Create account
        </button>
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
        className="block w-full rounded border border-border bg-bg px-2 py-1.5 font-mono text-[16px] text-fg placeholder:text-fg-dim focus:outline-none focus:ring-1 focus:ring-accent-gold sm:text-xs"
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
