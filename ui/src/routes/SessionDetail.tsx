import { useEffect, useState, type ReactNode } from "react";
import { useLocation, useNavigate, useParams } from "react-router";
import { Tabs, Skeleton, PageHeader } from "@ossrandom/design-system";
import { FeedStream } from "@/components/FeedStream";
import { CheckpointRow } from "@/components/CheckpointRow";
import { RevertSheet } from "@/components/RevertSheet";
import { DiffSheet } from "@/components/DiffSheet";
import { PaneView } from "@/components/PaneView";
import { fetchFeedHistory } from "@/hooks/useFeedHistory";
import { HealthDot, healthState } from "@/components/HealthDot";
import { AttentionLabel } from "@/components/AttentionLabel";
import { TokenBreakdown } from "@/components/TokenBreakdown";
import { LogDiskUsage } from "@/components/LogDiskUsage";
import { CostChart } from "@/components/CostChart";
import { SubagentTree } from "@/components/SubagentTree";
import { AgentTeamsPanel } from "@/components/AgentTeamsPanel";
import { SessionInputBar } from "@/components/SessionInputBar";
import { useSession, type Session } from "@/hooks/useSessions";
import { useCheckpoints, type Checkpoint } from "@/hooks/useCheckpoints";
import { relativeTime, shortenPath } from "@/lib/format";
import { cn } from "@/lib/utils";

interface SessionDetailProps {
  /** When true, rendered as a desktop right-pane (no full-page chrome). */
  embedded?: boolean;
}

type TabKey =
  | "feed"
  | "checkpoints"
  | "pane"
  | "subagents"
  | "teams"
  | "meta";

function tabFromPath(pathname: string): TabKey {
  if (pathname.endsWith("/feed")) return "feed";
  if (pathname.endsWith("/checkpoints")) return "checkpoints";
  if (pathname.endsWith("/pane")) return "pane";
  if (pathname.endsWith("/subagents")) return "subagents";
  if (pathname.endsWith("/teams")) return "teams";
  if (pathname.endsWith("/meta")) return "meta";
  return "pane";
}

/**
 * V4 + V17 + Meta. Tabs: feed (default), checkpoints, meta. URL drives
 * the active tab so deep-links + browser back behave correctly.
 */
export function SessionDetail({ embedded }: SessionDetailProps) {
  const { name } = useParams<{ name: string }>();
  const navigate = useNavigate();
  const location = useLocation();

  const { data: session, isLoading } = useSession(name);
  const tab = tabFromPath(location.pathname);

  function changeTab(next: TabKey) {
    if (!name) return;
    const base = `/s/${encodeURIComponent(name)}`;
    const target = next === "pane" ? base : `${base}/${next}`;
    navigate(target, { replace: true });
  }

  if (!name) return null;

  const attn = session?.attention;
  const attentive = Boolean(attn && attn.state !== "clear");

  return (
    <section
      aria-label={`Session ${name}`}
      data-attentive={attentive || undefined}
      className={cn(
        "flex min-h-0 flex-1 flex-col bg-bg",
        attentive && "border-l-[3px] border-alert-ember",
        !embedded && "min-h-screen",
      )}
    >
      <PageHeader
        size={embedded ? "xs" : "md"}
        className="shrink-0"
        backInline
        back={{ onClick: () => navigate("/"), label: "Back to dashboard" }}
        avatar={
          session ? (
            <HealthDot
              state={healthState({
                tmux_alive: session.tmux_alive,
                last_tool_call_at: session.last_tool_call_at,
              })}
            />
          ) : undefined
        }
        title={name}
        badge={
          session ? (
            <span
              className={cn(
                "shrink-0 text-[10px] font-semibold uppercase tracking-[0.18em]",
                session.mode === "yolo" ? "text-mode-yolo" : "text-mode-safe",
              )}
            >
              {session.mode}
            </span>
          ) : undefined
        }
        actions={
          attn && attn.state !== "clear" ? (
            <AttentionLabel attention={attn} className="hidden md:flex" />
          ) : undefined
        }
      />

      {session && (
        <div className="shrink-0">
          <TokenBreakdown tokens={session.tokens} />
        </div>
      )}

      <Tabs<TabKey>
        variant="line"
        size="md"
        scrollable
        value={tab}
        onChange={(k) => changeTab(k)}
        className="ctm-session-tabs flex min-h-0 flex-1 flex-col"
        items={[
            {
              key: "pane",
              label: "Pane",
              content: <PaneView sessionName={name} />,
            },
            {
              key: "feed",
              label: "Feed",
              content: <FeedTab sessionName={name} />,
            },
            {
              key: "checkpoints",
              label: "Checkpoints",
              content: <CheckpointsTab sessionName={name} />,
            },
            {
              key: "subagents",
              label: "Subagents",
              content: <SubagentTree sessionName={name} />,
            },
            {
              key: "teams",
              label: "Teams",
              content: <AgentTeamsPanel sessionName={name} />,
            },
            {
              key: "meta",
              label: "Meta",
              content: (
                <div className="flex-1 overflow-y-auto">
                  {isLoading && (
                    <div className="space-y-3 p-6">
                      <Skeleton className="h-4 w-2/3" />
                      <Skeleton className="h-4 w-1/2" />
                      <Skeleton className="h-4 w-3/4" />
                    </div>
                  )}
                  {session && (
                    <>
                      <MetaList session={session} />
                      <LogDiskUsage />
                      <CostChart sessionName={name} />
                    </>
                  )}
                </div>
              ),
          },
        ]}
      />
      {session && (
        <SessionInputBar
          sessionName={session.name}
          mode={session.mode}
        />
      )}
    </section>
  );
}

type FeedFilter = "all" | "bash";

const FEED_FILTER_STORAGE_PREFIX = "ctm.feed.filter.";

function readStoredFilter(sessionName: string): FeedFilter {
  if (typeof window === "undefined") return "all";
  try {
    const v = window.sessionStorage.getItem(
      FEED_FILTER_STORAGE_PREFIX + sessionName,
    );
    return v === "bash" ? "bash" : "all";
  } catch {
    return "all";
  }
}

/**
 * V10 — Feed tab with an `All | Bash` segmented filter. Persists the
 * selection in sessionStorage keyed by session name so refresh keeps
 * the user's last view. Filtering is purely client-side off the
 * existing feed cache; no backend change.
 */
function FeedTab({ sessionName }: { sessionName: string }) {
  const [filter, setFilter] = useState<FeedFilter>(() =>
    readStoredFilter(sessionName),
  );

  // Re-sync when the session changes (e.g. navigating between sessions
  // in the desktop two-pane layout remounts might not happen).
  useEffect(() => {
    setFilter(readStoredFilter(sessionName));
  }, [sessionName]);

  useEffect(() => {
    if (typeof window === "undefined") return;
    try {
      window.sessionStorage.setItem(
        FEED_FILTER_STORAGE_PREFIX + sessionName,
        filter,
      );
    } catch {
      // sessionStorage disabled — fine, UI still works, selection is
      // just not persisted across refreshes.
    }
  }, [filter, sessionName]);

  return (
    <>
      <div
        role="tablist"
        aria-label="Feed filter"
        className="flex shrink-0 gap-1 border-b border-border bg-bg px-4 py-2"
      >
        <FilterChip
          active={filter === "all"}
          onClick={() => setFilter("all")}
          label="All"
        />
        <FilterChip
          active={filter === "bash"}
          onClick={() => setFilter("bash")}
          label="Bash"
        />
      </div>
      <FeedStream
        sessionName={sessionName}
        bashOnly={filter === "bash"}
        onLoadOlder={(beforeId) => fetchFeedHistory(sessionName, beforeId)}
      />
    </>
  );
}

function FilterChip({
  active,
  onClick,
  label,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
}) {
  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={cn(
        "rounded-sm px-2.5 py-1 text-[11px] font-semibold uppercase tracking-[0.18em] transition-colors",
        active
          ? "bg-surface-2 text-fg"
          : "text-fg-muted hover:text-fg",
      )}
    >
      {label}
    </button>
  );
}

function CheckpointsTab({ sessionName }: { sessionName: string }) {
  const { data, isLoading, isError, error } = useCheckpoints(sessionName);
  const [selected, setSelected] = useState<Checkpoint | null>(null);
  // V18: separate DiffSheet state so the diff viewer and the revert
  // flow are fully independent — closing one must not affect the other.
  const [diffTarget, setDiffTarget] = useState<Checkpoint | null>(null);

  const checkpoints = data?.checkpoints ?? [];
  const isGitWorkdir = data?.git_workdir ?? true;

  return (
    <>
      <div className="min-h-0 flex-1 overflow-y-auto">
        {isLoading && (
          <div className="space-y-2 p-4">
            <Skeleton className="h-10 w-full" />
            <Skeleton className="h-10 w-full" />
          </div>
        )}
        {isError && (
          <p
            role="alert"
            className="m-4 border-l-[3px] border-alert-ember bg-surface px-3 py-2 text-sm text-alert-ember"
          >
            Could not load checkpoints
            {error instanceof Error ? `: ${error.message}` : ""}
          </p>
        )}
        {!isLoading && !isError && !isGitWorkdir && (
          <div className="m-4 border-l-[3px] border-accent-gold bg-surface px-3 py-3 text-sm text-fg">
            <div className="font-semibold">Checkpoints need a git repo</div>
            <p className="mt-1 text-[12px] text-fg-dim">
              This session&apos;s workdir isn&apos;t a git repository, so ctm has nothing to snapshot.
              Run{" "}
              <code className="rounded bg-surface-2 px-1 py-0.5 font-mono">git init</code>{" "}
              in the workdir to enable pre-yolo checkpoints on the next tool call.
            </p>
          </div>
        )}
        {!isLoading && !isError && isGitWorkdir && checkpoints.length === 0 && (
          <p className="px-4 py-8 text-center text-sm text-fg-dim">
            No checkpoints. Run ctm yolo to create the first.
          </p>
        )}
        <ul role="list">
          {checkpoints.map((cp) => (
            <li key={cp.sha}>
              <CheckpointRow
                checkpoint={cp}
                selected={selected?.sha === cp.sha}
                onSelect={setSelected}
                onViewDiff={setDiffTarget}
              />
            </li>
          ))}
        </ul>
      </div>
      <RevertSheet
        sessionName={sessionName}
        checkpoint={selected}
        onClose={() => setSelected(null)}
      />
      <DiffSheet
        sessionName={sessionName}
        checkpoint={diffTarget}
        onClose={() => setDiffTarget(null)}
      />
    </>
  );
}

function MetaList({ session }: { session: Session }) {
  const rows: Array<[label: string, value: ReactNode]> = [
    ["name", <code className="font-mono text-fg" key="n">{session.name}</code>],
    [
      "uuid",
      // break-all lets the UUID wrap on any character so narrow
      // viewports don't blow out the value column. all-small-caps
      // hyphens stay together visually so readability holds.
      <code className="break-all font-mono text-xs text-fg" key="u">
        {session.uuid}
      </code>,
    ],
    [
      "mode",
      <span className="uppercase tracking-[0.18em] text-fg" key="m">
        {session.mode}
      </span>,
    ],
    [
      "workdir",
      <code
        className="break-all font-mono text-xs text-fg"
        key="w"
        title={session.workdir}
      >
        {shortenPath(session.workdir, 5)}
      </code>,
    ],
    [
      "created",
      <time dateTime={session.created_at} key="c" title={session.created_at}>
        {relativeTime(session.created_at)} ago
      </time>,
    ],
    [
      "last attached",
      session.last_attached_at ? (
        <time
          dateTime={session.last_attached_at}
          key="la"
          title={session.last_attached_at}
        >
          {relativeTime(session.last_attached_at)} ago
        </time>
      ) : (
        <span className="text-fg-dim" key="la-none">
          never
        </span>
      ),
    ],
    [
      "last tool call",
      session.last_tool_call_at ? (
        <time dateTime={session.last_tool_call_at} key="lt">
          {relativeTime(session.last_tool_call_at)} ago
        </time>
      ) : (
        <span className="text-fg-dim" key="lt-none">
          none
        </span>
      ),
    ],
    [
      "context %",
      typeof session.context_pct === "number" ? (
        <span className="font-mono tabular-nums" key="ctx">
          {Math.round(session.context_pct)}%
        </span>
      ) : (
        <span className="text-fg-dim" key="ctx-none">
          —
        </span>
      ),
    ],
    [
      "tokens",
      session.tokens ? (
        <span
          className="flex flex-wrap gap-x-1.5 font-mono tabular-nums text-fg"
          key="tok"
          title={`in ${session.tokens.input_tokens} · out ${session.tokens.output_tokens} · cache ${session.tokens.cache_tokens}`}
        >
          <span>{session.tokens.input_tokens.toLocaleString()}</span>
          <span className="text-fg-muted">·</span>
          <span>{session.tokens.output_tokens.toLocaleString()}</span>
          <span className="text-fg-muted">·</span>
          <span>{session.tokens.cache_tokens.toLocaleString()}</span>
        </span>
      ) : (
        <span className="text-fg-dim" key="tok-none">
          —
        </span>
      ),
    ],
    [
      "attention",
      session.attention && session.attention.state !== "clear" ? (
        <AttentionLabel attention={session.attention} key="att" />
      ) : (
        <span className="text-fg-dim" key="att-none">
          clear
        </span>
      ),
    ],
  ];

  return (
    <dl
      className={cn(
        "grid gap-x-6 gap-y-3 px-6 py-6",
        // Label above value on mobile (clean & lets values breathe);
        // side-by-side at sm+ where the label column fits without
        // crushing long values like the UUID or token tuple.
        "grid-cols-1 sm:grid-cols-[max-content_1fr]",
      )}
    >
      {rows.map(([k, v]) => (
        <div
          key={k}
          className="flex flex-col gap-y-1 sm:contents sm:gap-y-0"
        >
          <dt className="text-[11px] font-semibold uppercase tracking-[0.18em] text-fg-muted">
            {k}
          </dt>
          <dd className="min-w-0 text-sm text-fg">{v}</dd>
        </div>
      ))}
    </dl>
  );
}
