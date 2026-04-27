import { useState } from "react";
import { ChevronRight } from "lucide-react";
import { Skeleton } from "@ossrandom/design-system";
import { useTeams, type Team, type TeamMember } from "@/hooks/useTeams";
import { relativeTime } from "@/lib/format";
import { cn } from "@/lib/utils";

/**
 * V16 — Agent teams panel.
 *
 * Renders each team as a collapsible card. Header carries the team
 * name + member count + status chip; expanding reveals the member
 * list and any lead-agent summary (once the backend starts surfacing
 * one — it's null today and the blockquote collapses cleanly).
 *
 * Refetches on `team_spawn` / `team_settled` SSE events via the
 * shared queryKey invalidation in SseProvider.
 */
export function AgentTeamsPanel({ sessionName }: { sessionName: string }) {
  const { data, isLoading, isError, error } = useTeams(sessionName);
  const teams = data?.teams ?? [];

  return (
    <section
      aria-label="Agent teams"
      className="min-h-0 flex-1 overflow-y-auto p-4"
    >
      {isLoading && (
        <div className="space-y-3">
          <Skeleton className="h-14 w-full" />
          <Skeleton className="h-14 w-full" />
        </div>
      )}

      {isError && (
        <p
          role="alert"
          className="border-l-[3px] border-alert-ember bg-surface px-3 py-2 text-sm text-alert-ember"
        >
          Could not load teams
          {error instanceof Error ? `: ${error.message}` : ""}
        </p>
      )}

      {!isLoading && !isError && teams.length === 0 && (
        <p className="py-8 text-center text-sm text-fg-dim">
          No teams for this session.
        </p>
      )}

      <ul role="list" className="space-y-3">
        {teams.map((team) => (
          <li key={team.id}>
            <TeamCard team={team} />
          </li>
        ))}
      </ul>
    </section>
  );
}

function TeamCard({ team }: { team: Team }) {
  const [open, setOpen] = useState(false);
  return (
    <article
      data-testid={`team-card-${team.id}`}
      className="border border-border bg-surface"
    >
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className={cn(
          "flex w-full items-center gap-3 px-4 py-3 text-left",
          "hover:bg-surface-2 focus-visible:bg-surface-2",
          "focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent-gold",
        )}
      >
        <ChevronRight
          size={14}
          aria-hidden
          className={cn(
            "shrink-0 text-fg-muted transition-transform",
            open && "rotate-90",
          )}
        />
        <StatusChip status={team.status} />
        <h3 className="min-w-0 flex-1 truncate text-xs font-semibold text-fg">
          {team.name}
        </h3>
        <span className="shrink-0 font-mono text-[11px] tabular-nums text-fg-dim">
          {team.members.length} members
        </span>
        <time
          dateTime={team.dispatched_at}
          className="shrink-0 font-mono text-[11px] tabular-nums text-fg-dim"
        >
          {relativeTime(team.dispatched_at)} ago
        </time>
      </button>
      {open && (
        <div
          data-testid={`team-detail-${team.id}`}
          className="border-t border-border bg-bg px-4 py-3"
        >
          {team.summary && (
            <blockquote className="mb-3 border-l-[3px] border-accent-gold bg-surface px-3 py-2 text-sm italic text-fg">
              {team.summary}
            </blockquote>
          )}
          <ul role="list" className="space-y-1">
            {team.members.map((m) => (
              <TeamMemberRow key={m.subagent_id} member={m} />
            ))}
          </ul>
        </div>
      )}
    </article>
  );
}

function TeamMemberRow({ member }: { member: TeamMember }) {
  return (
    <li
      className="flex items-center gap-3 py-1 text-sm"
      data-testid={`team-member-${member.subagent_id}`}
    >
      <StatusDot status={member.status} />
      <code className="shrink-0 font-mono text-[11px] text-fg-muted">
        {member.subagent_id.slice(0, 10)}
      </code>
      <span className="min-w-0 flex-1 truncate text-fg">
        {member.description || <span className="text-fg-dim">—</span>}
      </span>
    </li>
  );
}

function StatusChip({ status }: { status: Team["status"] }) {
  const label =
    status === "running"
      ? "Running"
      : status === "failed"
      ? "Failed"
      : "Completed";
  return (
    <span
      data-testid={`team-status-${status}`}
      aria-label={label}
      className={cn(
        "inline-flex shrink-0 items-center gap-1.5 rounded-sm px-2 py-0.5",
        "text-[10px] font-semibold uppercase tracking-[0.18em]",
        status === "running" && "bg-live-dot/20 text-live-dot",
        status === "completed" && "bg-fg-muted/20 text-fg-muted",
        status === "failed" && "bg-alert-ember/20 text-alert-ember",
      )}
    >
      <StatusDot status={status} />
      {label}
    </span>
  );
}

function StatusDot({ status }: { status: Team["status"] }) {
  return (
    <span
      aria-hidden
      className={cn(
        "inline-block h-2 w-2 rounded-full",
        status === "running" && "animate-pulse bg-live-dot",
        status === "completed" && "bg-fg-muted",
        status === "failed" && "bg-alert-ember",
      )}
    />
  );
}
