import { useEffect, useState } from "react";
import { useNavigate, useParams } from "react-router";
import { Settings, SquarePlus, Stethoscope } from "lucide-react";
import { PageHeader, IconButton } from "@ossrandom/design-system";
import { CostChart } from "@/components/CostChart";
import { NewSessionModal } from "@/components/NewSessionModal";
import { QuotaStrip } from "@/components/QuotaStrip";
import { SessionListPanel } from "@/components/SessionListPanel";
import { SettingsDrawer } from "@/components/SettingsDrawer";
import { ThemeToggle } from "@/components/ThemeToggle";
import { SessionDetail } from "@/routes/SessionDetail";
import { useRecentWorkdirs } from "@/hooks/useRecentWorkdirs";
import { sortSessions, useSessions } from "@/hooks/useSessions";
import { cn } from "@/lib/utils";

/**
 * Dashboard (V1 + V12). Single route owner for `/`, `/s/:name` and the
 * `/s/:name/{checkpoints,meta}` tab variants. Layout is responsive,
 * not route-driven, so the list does not unmount when the user picks a
 * session in two-pane mode (spec §3 Desktop scaling B).
 *
 *   >=768px    list (300px) | SessionDetail (auto-selects latest)
 *   <768px     list-only when no name; detail-only when name is set
 *
 * Height model: root is `h-dvh` at every breakpoint so the header +
 * QuotaStrip stay pinned at top and the Live-feed footer stays pinned
 * at bottom, with the session list scrolling inside. `h-dvh` (dynamic
 * viewport) instead of `h-screen` (100vh) is critical on mobile —
 * 100vh includes the collapsible browser chrome, which would push
 * the footer below the visible area until the user scrolls once and
 * the address bar collapses. `dvh` tracks the actual visible viewport.
 *
 * The middle flex row is `flex-1 min-h-0 overflow-hidden` so the
 * list pane and the detail pane each own their own scroll container
 * and never push the page height. Without `min-h-0` the flex →
 * overflow chain breaks and children can't scroll.
 */
export function Dashboard() {
  const { name } = useParams<{ name?: string }>();
  const navigate = useNavigate();
  const { data: sessions } = useSessions();
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [newSessionOpen, setNewSessionOpen] = useState(false);
  const recentWorkdirs = useRecentWorkdirs();

  // Desktop-only: when nothing is selected, auto-navigate to the top
  // active session. Uses the same sortSessions order as the list so
  // "latest" matches the top card visually. Mobile is left alone —
  // users there are expected to land on the list and pick.
  useEffect(() => {
    if (name) return;
    if (!sessions || sessions.length === 0) return;
    if (globalThis.window === undefined) return;
    if (!globalThis.matchMedia("(min-width: 768px)").matches) return;
    const top = sessions
      .filter((s) => s.is_active)
      .slice()
      .sort(sortSessions)[0];
    if (top) {
      navigate(`/s/${encodeURIComponent(top.name)}`, { replace: true });
    }
  }, [name, sessions, navigate]);

  const detailVisible = Boolean(name);

  return (
    <div className="flex h-dvh flex-col bg-bg text-fg">
      <PageHeader
        size="sm"
        inlineSubtitle
        className="shrink-0"
        title="ctm"
        subtitle="claude tmux manager"
        actions={
          <>
            <IconButton
              size="sm"
              variant="ghost"
              aria-label="Open doctor diagnostics"
              icon={<Stethoscope size={14} aria-hidden />}
              onClick={() => navigate("/doctor")}
            />
            <IconButton
              size="sm"
              variant="ghost"
              aria-label="New session"
              icon={<SquarePlus size={14} aria-hidden />}
              onClick={() => setNewSessionOpen(true)}
            />
            <IconButton
              size="sm"
              variant="ghost"
              aria-label="Open settings"
              icon={<Settings size={14} aria-hidden />}
              onClick={() => setSettingsOpen(true)}
            />
            <ThemeToggle />
          </>
        }
      />
      <SettingsDrawer
        open={settingsOpen}
        onClose={() => setSettingsOpen(false)}
      />
      <NewSessionModal
        open={newSessionOpen}
        onClose={() => setNewSessionOpen(false)}
        recents={recentWorkdirs}
      />

      <div className="shrink-0">
        <QuotaStrip />
      </div>

      <div className="hidden shrink-0 md:block">
        <CostChart />
      </div>

      <div className="flex min-h-0 flex-1 overflow-hidden">
        <SessionListPanel
          activeName={name}
          className={cn(
            "md:flex md:w-[300px] md:shrink-0",
            detailVisible ? "hidden md:flex" : "flex w-full",
          )}
        />
        <main
          className={cn(
            "min-h-0 min-w-0 flex-1 flex-col overflow-hidden",
            "md:flex",
            detailVisible ? "flex" : "hidden md:flex",
          )}
        >
          {name ? <SessionDetail embedded /> : <EmptyDetail />}
        </main>
      </div>
    </div>
  );
}

function EmptyDetail() {
  return (
    <div className="flex flex-1 items-center justify-center px-6 py-12">
      <div className="max-w-sm space-y-2 text-center">
        <p className="text-[11px] font-semibold uppercase tracking-[0.18em] text-fg-muted">
          No session selected
        </p>
        <p className="text-sm text-fg-dim">
          Pick a session from the list to see its live tool-call feed,
          checkpoints, and metadata.
        </p>
      </div>
    </div>
  );
}
