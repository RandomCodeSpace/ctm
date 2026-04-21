/**
 * Shared colour mapping for quota / context-window progress bars.
 *
 * Thresholds match the V1 `QuotaStrip` design:
 *   >= 90 → ember (critical)
 *   >= 75 → gold  (warning)
 *   else  → muted (nominal)
 *
 * Returns a Tailwind background-colour class so callers can drop it
 * directly into a `cn()` call.
 */
export function pctColor(pct: number): string {
  if (pct >= 90) return "bg-alert-ember";
  if (pct >= 75) return "bg-accent-gold";
  return "bg-fg-muted";
}
