import { useEffect, useState, type ReactNode } from "react";
import { Link, useLocation, useNavigate, useParams } from "react-router";
import { ArrowLeft } from "lucide-react";
import {
  Tabs,
  TabsContent,
  TabsList,
  TabsTrigger,
} from "@/components/ui/tabs";
import { Skeleton } from "@/components/ui/skeleton";
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
  if (pathname.endsWith("/checkpoints")) return "checkpoints";
  if (pathname.endsWith("/pane")) return "pane";
  if (pathname.endsWith("/subagents")) return "subagents";
  if (pathname.endsWith("/teams")) return "teams";
  if (pathname.endsWith("/meta")) return "meta";
  return "feed";
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
    const target = next === "feed" ? base : `${base}/${next}`;
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
      <header
        className={cn(
          "flex shrink-0 items-baseline gap-3 border-b border-border px-4 py-3",
          embedded ? "" : "px-6",
        )}
      >
        <Link
          to="/"
          aria-label="Back to dashboard"
          className={cn(
            "self-center text-fg-dim hover:text-fg",
            // Always visible when not embedded; mobile-only when embedded
            // (desktop two-pane keeps the list visible so back is moot).
            embedded ? "md:hidden" : "",
          )}
        >
          <ArrowLeft size={16} aria-hidden />
        </Link>
        {session && (
          <HealthDot
            state={healthState({
              tmux_alive: session.tmux_alive,
              last_tool_call_at: session.last_tool_call_at,
            })}
            className="self-center"
          />
        )}
        <h2 className="truncate font-serif text-lg font-semibold text-fg">
          {name}
        </h2>
        {session && (
          <span
            className={cn(
              "shrink-0 text-[10px] font-semibold uppercase tracking-[0.18em]",
              session.mode === "yolo" ? "text-mode-yolo" : "text-mode-safe",
            )}
          >
            {session.mode}
          </span>
        )}
        {attn && attn.state !== "clear" && (
          <AttentionLabel attention={attn} className="ml-2 hidden md:flex" />
        )}
      </header>

      {session && (
        <div className="shrink-0">
          <TokenBreakdown tokens={session.tokens} />
        </div>
      )}

      <Tabs
        value={tab}
        onValueChange={(v) => changeTab(v as TabKey)}
        className="flex min-h-0 flex-1 flex-col gap-0"
      >
        <TabsList className="h-auto shrink-0 justify-start rounded-none border-b border-border bg-bg px-4 py-0">
          <TabTrigger value="feed">Feed</TabTrigger>
          <TabTrigger value="checkpoints">Checkpoints</TabTrigger>
          <TabTrigger value="subagents">Subagents</TabTrigger>
          <TabTrigger value="teams">Teams</TabTrigger>
          <TabTrigger value="pane">Pane</TabTrigger>
          <TabTrigger value="meta">Meta</TabTrigger>
        </TabsList>

        <TabsContent value="feed" className="m-0 flex min-h-0 flex-1 flex-col">
          <FeedTab sessionName={name} />
        </TabsContent>

        <TabsContent
          value="checkpoints"
          className="m-0 flex min-h-0 flex-1 flex-col"
        >
          <CheckpointsTab sessionName={name} />
        </TabsContent>

        <TabsContent
          value="subagents"
          className="m-0 flex min-h-0 flex-1 flex-col"
        >
          <SubagentTree sessionName={name} />
        </TabsContent>

        <TabsContent
          value="teams"
          className="m-0 flex min-h-0 flex-1 flex-col"
        >
          <AgentTeamsPanel sessionName={name} />
        </TabsContent>

        <TabsContent
          value="pane"
          className="m-0 flex min-h-0 flex-1 flex-col"
        >
          <PaneView sessionName={name} />
        </TabsContent>

        <TabsContent value="meta" className="m-0 flex-1 overflow-y-auto">
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
        </TabsContent>
      </Tabs>
    </section>
  );
}

function TabTrigger({
  value,
  children,
}: {
  value: string;
  children: ReactNode;
}) {
  return (
    <TabsTrigger
      value={value}
      className={cn(
        "rounded-none border-0 border-b-2 border-transparent bg-transparent px-3 py-2",
        "text-[11px] font-semibold uppercase tracking-[0.18em] text-fg-muted",
        "data-[state=active]:border-accent-gold data-[state=active]:bg-transparent",
        "data-[state=active]:text-fg data-[state=active]:shadow-none",
      )}
    >
      {children}
    </TabsTrigger>
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
        {!isLoading && !isError && (data ?? []).length === 0 && (
          <p className="px-4 py-8 text-center text-sm text-fg-dim">
            No checkpoints. Run ctm yolo to create the first.
          </p>
        )}
        <ul role="list">
          {(data ?? []).map((cp) => (
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
      <code className="font-mono text-xs text-fg" key="u">
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
        className="font-mono text-xs text-fg"
        key="w"
        title={session.workdir}
      >
        {shortenPath(session.workdir, 5)}
      </code>,
    ],
    [
      "created",
      <time dateTime={session.created_at} key="c">
        {relativeTime(session.created_at)} ago ({session.created_at})
      </time>,
    ],
    [
      "last attached",
      session.last_attached_at ? (
        <time dateTime={session.last_attached_at} key="la">
          {relativeTime(session.last_attached_at)} ago ({session.last_attached_at})
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
      "tokens (in/out/cache)",
      session.tokens ? (
        <span className="font-mono tabular-nums text-fg" key="tok">
          {session.tokens.input_tokens.toLocaleString()}
          <span className="px-1 text-fg-muted">·</span>
          {session.tokens.output_tokens.toLocaleString()}
          <span className="px-1 text-fg-muted">·</span>
          {session.tokens.cache_tokens.toLocaleString()}
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
    <dl className="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-3 px-6 py-6">
      {rows.map(([k, v]) => (
        <div key={k} className="contents">
          <dt className="text-[11px] font-semibold uppercase tracking-[0.18em] text-fg-muted">
            {k}
          </dt>
          <dd className="min-w-0 text-sm text-fg">{v}</dd>
        </div>
      ))}
    </dl>
  );
}
