/**
 * Display formatters. All pure, no side effects.
 */

// eslint-disable-next-line no-control-regex -- ANSI escape sequences begin with U+001B by definition.
const ANSI_RE = /\x1B\[[0-9;]*[A-Za-z]/g;

/** Strip ANSI escape codes from a string (for tool output preview). */
export function stripAnsi(s: string): string {
  return s.replace(ANSI_RE, "");
}

/** "12 sec", "3 min", "5 hr", "2 days" — coarse but readable. */
export function relativeTime(iso: string | Date, now: Date = new Date()): string {
  const then = typeof iso === "string" ? new Date(iso) : iso;
  const seconds = Math.max(0, Math.round((now.getTime() - then.getTime()) / 1000));
  return formatCoarseDuration(seconds);
}

/**
 * Same coarse-duration formatter as `relativeTime`, but counts forward
 * to a future timestamp: "in 3 hr". Used for things like quota reset
 * timers where `iso` is in the future; relativeTime would clamp those
 * to "0 sec" because it measures how long *ago* the timestamp was.
 */
export function relativeFuture(iso: string | Date, now: Date = new Date()): string {
  const then = typeof iso === "string" ? new Date(iso) : iso;
  const seconds = Math.max(0, Math.round((then.getTime() - now.getTime()) / 1000));
  return formatCoarseDuration(seconds);
}

function formatCoarseDuration(seconds: number): string {
  if (seconds < 60) return `${seconds} sec`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 60) return `${minutes} min`;
  const hours = Math.round(minutes / 60);
  if (hours < 24) return `${hours} hr`;
  const days = Math.round(hours / 24);
  return `${days} day${days === 1 ? "" : "s"}`;
}

/** 1234 → "1.2k", 2_500_000 → "2.5M". */
export function compactNumber(n: number): string {
  const abs = Math.abs(n);
  if (abs >= 1e9) return `${(n / 1e9).toFixed(1).replace(/\.0$/, "")}B`;
  if (abs >= 1e6) return `${(n / 1e6).toFixed(1).replace(/\.0$/, "")}M`;
  if (abs >= 1e3) return `${(n / 1e3).toFixed(1).replace(/\.0$/, "")}k`;
  return String(n);
}

/**
 * Shorten a long path: "/home/dev/projects/ctm/internal/serve/events.go"
 * → ".../serve/events.go" when > maxSegments tail segments.
 */
export function shortenPath(p: string, maxSegments = 3): string {
  if (!p) return p;
  const parts = p.split("/").filter(Boolean);
  if (parts.length <= maxSegments) return p;
  return ".../" + parts.slice(-maxSegments).join("/");
}

/**
 * Human-readable byte size. Binary units (1024-based) because ctm users
 * are devs — "1 MB" here means 1 MiB. Picks the smallest unit that keeps
 * the scaled value < 1024 so "1023 KB" never degrades to "0.9 MB".
 *
 *   0        → "0 B"
 *   1024     → "1 KB"
 *   1048576  → "1 MB"
 *   1572864  → "1.5 MB"
 */
export function humanBytes(bytes: number): string {
  if (!Number.isFinite(bytes)) return "—";
  const abs = Math.abs(bytes);
  if (abs < 1024) return `${bytes} B`;
  const units = ["KB", "MB", "GB", "TB", "PB"];
  let value = bytes / 1024;
  let unitIdx = 0;
  while (Math.abs(value) >= 1024 && unitIdx < units.length - 1) {
    value /= 1024;
    unitIdx++;
  }
  // Whole numbers render without a trailing ".0"; otherwise keep one
  // decimal so "1.5 MB" stays readable.
  const scaled = Number.isInteger(value) ? value.toString() : value.toFixed(1);
  return `${scaled} ${units[unitIdx]}`;
}
