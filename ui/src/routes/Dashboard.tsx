import { useEffect } from "react";
import { useNavigate, useParams } from "react-router";
import { QuotaStrip } from "@/components/QuotaStrip";
import { SessionListPanel } from "@/components/SessionListPanel";
import { ThemeToggle } from "@/components/ThemeToggle";
import { SessionDetail } from "@/routes/SessionDetail";
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
 * Height model: root is h-screen (exactly viewport). The middle flex
 * row is flex-1 min-h-0 overflow-hidden so the list pane and the
 * detail pane each own their own scroll container and never push the
 * page height. `min-h-screen` would let the root grow past the
 * viewport, breaking the flex → overflow chain for scroll children.
 */
export function Dashboard() {
  const { name } = useParams<{ name?: string }>();
  const navigate = useNavigate();
  const { data: sessions } = useSessions();

  // Desktop-only: when nothing is selected, auto-navigate to the top
  // active session. Uses the same sortSessions order as the list so
  // "latest" matches the top card visually. Mobile is left alone —
  // users there are expected to land on the list and pick.
  useEffect(() => {
    if (name) return;
    if (!sessions || sessions.length === 0) return;
    if (typeof window === "undefined") return;
    if (!window.matchMedia("(min-width: 768px)").matches) return;
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
    <div className="flex h-screen flex-col bg-bg text-fg">
      <header className="flex shrink-0 items-center justify-between border-b border-border px-4 py-3">
        <h1 className="font-serif text-xl font-bold tracking-tight">ctm</h1>
        <ThemeToggle />
      </header>

      <div className="shrink-0">
        <QuotaStrip />
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
