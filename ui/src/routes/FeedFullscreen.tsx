import { Link } from "react-router";
import { ArrowLeft } from "lucide-react";
import { FeedStream } from "@/components/FeedStream";
import { ThemeToggle } from "@/components/ThemeToggle";

/**
 * V5 — merged live feed across all sessions. Same FeedStream component,
 * unfiltered (consumes the cross-session cache populated by SseProvider
 * from /events/all tool_call events).
 */
export function FeedFullscreen() {
  return (
    <div className="flex min-h-screen flex-col bg-bg text-fg">
      <header className="flex items-center justify-between border-b border-border px-4 py-3">
        <div className="flex items-center gap-3">
          <Link
            to="/"
            aria-label="Back to dashboard"
            className="text-fg-dim hover:text-fg"
          >
            <ArrowLeft size={16} aria-hidden />
          </Link>
          <h1 className="font-serif text-xl font-bold tracking-tight">
            Live feed
          </h1>
          <span className="text-[11px] font-semibold uppercase tracking-[0.18em] text-fg-muted">
            All sessions
          </span>
        </div>
        <ThemeToggle />
      </header>
      <div className="flex min-h-0 flex-1 flex-col">
        <FeedStream />
      </div>
    </div>
  );
}
