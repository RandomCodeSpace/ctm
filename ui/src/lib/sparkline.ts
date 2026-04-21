/**
 * Pure bucketing helper for tool-frequency sparklines. Kept framework-
 * agnostic so it's trivially unit-testable and reusable if another
 * surface wants the same histogram.
 */

export interface BucketOptions {
  now: number;
  windowMs: number;
  buckets: number;
}

/**
 * Count entries per bucket. Events older than (now - windowMs) are
 * dropped. Events at/after now are counted in the latest bucket. The
 * returned array has `buckets` entries ordered oldest-first.
 */
export function bucketize(
  timestamps: number[],
  { now, windowMs, buckets }: BucketOptions,
): number[] {
  if (buckets <= 0 || windowMs <= 0) return [];
  const out = new Array<number>(buckets).fill(0);
  const bucketMs = windowMs / buckets;
  const oldest = now - windowMs;
  for (const t of timestamps) {
    if (t < oldest || t > now) continue;
    const offset = t - oldest;
    let idx = Math.floor(offset / bucketMs);
    if (idx >= buckets) idx = buckets - 1;
    if (idx < 0) idx = 0;
    out[idx] += 1;
  }
  return out;
}
