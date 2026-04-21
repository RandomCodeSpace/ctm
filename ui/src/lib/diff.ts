/**
 * Shared unified-diff line classifier.
 *
 * Single source of truth for the `+/-/@@/plain` → Tailwind colour
 * mapping used by both:
 *
 *   - DiffSheet (V18 checkpoint diff, full `git show` output)
 *   - ToolCallRow (V9 inline Edit/MultiEdit/Write detail)
 *
 * Keeping this in one module means the two views stay visually
 * consistent automatically — if we ever swap emerald-400 for something
 * softer, both render paths follow.
 */
export function classifyLine(line: string): string {
  if (line.startsWith("@@")) return "text-fg-dim";
  if (line.startsWith("+")) return "text-emerald-400";
  if (line.startsWith("-")) return "text-alert-ember";
  return "text-fg";
}
