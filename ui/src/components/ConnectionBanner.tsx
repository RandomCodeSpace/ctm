import { useSseStatus } from "@/components/SseProvider";

/**
 * 1-line ember-red strip across viewport top, persistent until SSE
 * reconnects. Spec §5 attention surfacing.
 */
export function ConnectionBanner() {
  const { connected } = useSseStatus();
  if (connected) return null;
  return (
    <div
      role="status"
      aria-live="polite"
      className="sticky top-0 z-50 flex h-7 items-center justify-center bg-alert-ember px-3 text-xs font-medium uppercase tracking-[0.18em] text-white"
    >
      Connection lost — retrying
    </div>
  );
}
