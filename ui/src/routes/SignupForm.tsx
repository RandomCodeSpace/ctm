import { useState } from "react";
import { ApiError } from "@/lib/api";
import { useSignup } from "@/hooks/useSignup";
import { AuthField } from "@/components/auth/AuthField";
import { serverMessage } from "@/components/auth/serverMessage";

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
      <h1 className="mb-4 text-lg font-bold sm:text-xl">Create your ctm account</h1>
      <form onSubmit={onSubmit} className="space-y-3">
        <AuthField label="Email" type="email" value={username} onChange={setUsername} autoComplete="email" />
        <AuthField label="Password" type="password" value={password} onChange={setPassword} autoComplete="new-password" />
        <AuthField label="Confirm password" type="password" value={confirm} onChange={setConfirm} autoComplete="new-password" />
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
