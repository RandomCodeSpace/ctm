import { useState } from "react";
import { Link } from "react-router";
import { ChevronDown, ChevronRight, RefreshCw } from "lucide-react";
import { ThemeToggle } from "@/components/ThemeToggle";
import { Skeleton } from "@/components/ui/skeleton";
import {
  useDoctor,
  type DoctorCheck,
  type DoctorStatus,
} from "@/hooks/useDoctor";
import { cn } from "@/lib/utils";

/**
 * V20 diagnostics panel. Mirrors `ctm doctor` CLI output in the web UI
 * so users can triage from the browser. Each row is a single check:
 * dot (ok/warn/err), name, message. Rows with a remediation are
 * expandable to reveal the fix text.
 *
 * Visual parity with SessionListPanel: same header typography
 * (uppercase 0.18em tracking), same border-b between rows, same
 * tight padding per row.
 */
export function DoctorPanel() {
  const { data, isLoading, isError, error, refetch, isFetching } = useDoctor();

  return (
    <div className="flex h-dvh flex-col bg-bg text-fg">
      <header className="flex shrink-0 items-center justify-between border-b border-border px-4 py-3">
        <div className="flex items-center gap-3">
          <Link
            to="/"
            className="font-serif text-xl font-bold tracking-tight text-fg hover:text-fg-muted transition-colors"
          >
            ctm
          </Link>
          <span
            aria-hidden
            className="text-[11px] font-semibold uppercase tracking-[0.18em] text-fg-muted"
          >
            / doctor
          </span>
        </div>
        <ThemeToggle />
      </header>

      <main className="flex-1 min-h-0 overflow-y-auto">
        <div className="mx-auto max-w-3xl">
          <div className="flex items-center justify-between border-b border-border px-4 py-3">
            <h2 className="text-[11px] font-semibold uppercase tracking-[0.18em] text-fg-muted">
              Diagnostics
            </h2>
            <button
              type="button"
              onClick={() => refetch()}
              disabled={isFetching}
              className={cn(
                "inline-flex items-center gap-1.5 rounded px-2 py-1 text-xs text-fg-muted",
                "transition-colors hover:bg-surface-2 hover:text-fg",
                "disabled:cursor-not-allowed disabled:opacity-50",
              )}
              aria-label="Re-run checks"
            >
              <RefreshCw
                size={12}
                className={cn(isFetching && "animate-spin")}
                aria-hidden
              />
              Re-run checks
            </button>
          </div>

          {isLoading && (
            <div className="space-y-px px-4 py-4">
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
              <Skeleton className="h-10 w-full" />
            </div>
          )}

          {isError && (
            <p
              role="alert"
              className="m-4 border-l-[3px] border-alert-ember bg-surface px-3 py-2 text-sm text-alert-ember"
            >
              Could not load diagnostics
              {error instanceof Error ? `: ${error.message}` : ""}
            </p>
          )}

          {!isLoading && !isError && data && data.length === 0 && (
            <p className="px-4 py-8 text-center text-sm text-fg-dim">
              No checks reported.
            </p>
          )}

          {!isLoading && !isError && data && data.length > 0 && (
            <ul role="list" className="divide-y divide-border">
              {data.map((check, i) => (
                <li key={`${check.name}-${i}`}>
                  <DoctorRow check={check} />
                </li>
              ))}
            </ul>
          )}
        </div>
      </main>
    </div>
  );
}

interface DoctorRowProps {
  check: DoctorCheck;
}

function DoctorRow({ check }: DoctorRowProps) {
  const hasRemediation = Boolean(check.remediation);
  const [expanded, setExpanded] = useState(false);

  const toggle = () => {
    if (hasRemediation) setExpanded((v) => !v);
  };

  return (
    <div className={cn("bg-surface")}>
      <button
        type="button"
        onClick={toggle}
        disabled={!hasRemediation}
        aria-expanded={hasRemediation ? expanded : undefined}
        aria-label={`${check.name}: ${check.status}${check.message ? ` — ${check.message}` : ""}`}
        className={cn(
          "flex w-full items-start gap-3 px-4 py-3 text-left",
          "transition-colors",
          hasRemediation && "cursor-pointer hover:bg-surface-2",
          !hasRemediation && "cursor-default",
        )}
      >
        <StatusDot status={check.status} />
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <div className="flex items-center gap-2">
            <span className="truncate font-mono text-xs text-fg">
              {check.name}
            </span>
            <span
              className={cn(
                "text-[10px] font-semibold uppercase tracking-[0.14em]",
                statusLabelClass(check.status),
              )}
            >
              {check.status}
            </span>
          </div>
          {check.message && (
            <span className="truncate text-xs text-fg-dim">
              {check.message}
            </span>
          )}
        </div>
        {hasRemediation && (
          <span className="shrink-0 pt-0.5 text-fg-muted">
            {expanded ? (
              <ChevronDown size={14} aria-hidden />
            ) : (
              <ChevronRight size={14} aria-hidden />
            )}
          </span>
        )}
      </button>
      {hasRemediation && expanded && (
        <div className="border-t border-border bg-bg px-4 py-3 pl-10">
          <p className="text-[11px] font-semibold uppercase tracking-[0.14em] text-fg-muted">
            Remediation
          </p>
          <p className="mt-1 text-xs text-fg-dim">{check.remediation}</p>
        </div>
      )}
    </div>
  );
}

interface StatusDotProps {
  status: DoctorStatus;
}

function StatusDot({ status }: StatusDotProps) {
  return (
    <span
      role="status"
      aria-label={`check ${status}`}
      data-status={status}
      className={cn(
        "mt-1.5 inline-block h-2 w-2 shrink-0 rounded-full",
        statusDotClass(status),
      )}
    />
  );
}

function statusDotClass(status: DoctorStatus): string {
  switch (status) {
    case "ok":
      return "bg-live-dot";
    case "warn":
      return "bg-accent-gold";
    case "err":
      return "bg-alert-ember";
    default:
      return "bg-fg-muted";
  }
}

function statusLabelClass(status: DoctorStatus): string {
  switch (status) {
    case "ok":
      return "text-live-dot";
    case "warn":
      return "text-accent-gold";
    case "err":
      return "text-alert-ember";
    default:
      return "text-fg-muted";
  }
}
