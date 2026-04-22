import { useState } from "react";
import { useAuthStatus } from "@/hooks/useAuthStatus";
import { SignupForm } from "@/routes/SignupForm";
import { LoginForm } from "@/routes/LoginForm";

interface Props {
  children: React.ReactNode;
}

export function AuthGate({ children }: Props) {
  const status = useAuthStatus();
  const [override, setOverride] = useState<"signup" | "login" | null>(null);

  if (status.isLoading) {
    return (
      <div className="flex h-dvh items-center justify-center text-fg-dim">
        <div className="text-xs uppercase tracking-[0.18em]">Loading…</div>
      </div>
    );
  }
  if (status.error) {
    return (
      <div className="mx-auto mt-16 w-full max-w-sm rounded-lg border border-border bg-surface p-6 text-alert-ember">
        Could not reach the daemon. Try refreshing.
      </div>
    );
  }

  const s = status.data!;
  const showLogin =
    override === "login" || (override !== "signup" && s.registered && !s.authenticated);
  const showSignup =
    override === "signup" || (override !== "login" && !s.registered);

  if (showSignup) {
    return <SignupForm onSwitchToLogin={() => setOverride("login")} />;
  }
  if (showLogin) {
    return <LoginForm onSwitchToSignup={() => setOverride("signup")} />;
  }
  return <>{children}</>;
}
