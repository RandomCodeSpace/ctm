import { useMemo } from "react";
import { useFeed } from "@/hooks/useFeed";
import { bucketize } from "@/lib/sparkline";

const WIDTH = 80;
const HEIGHT = 14;
const BUCKETS = 20;
const WINDOW_MS = 10 * 60 * 1000; // 10 minutes

interface Props {
  sessionName: string;
}

/**
 * Inline ~80×14 SVG histogram of tool-call frequency over the last
 * 10 min for one session. Pulls from the existing feed cache — zero
 * backend contract. Renders nothing when the session has no cached
 * tool calls (keeps fresh cards clean).
 */
export function ToolFrequencySparkline({ sessionName }: Props) {
  const { data } = useFeed(sessionName);

  const bars = useMemo(() => {
    if (!data || data.length === 0) return null;
    const now = Date.now();
    const timestamps = data
      .map((row) => Date.parse(row.ts))
      .filter((t) => !Number.isNaN(t));
    const counts = bucketize(timestamps, {
      now,
      windowMs: WINDOW_MS,
      buckets: BUCKETS,
    });
    const peak = Math.max(1, ...counts);
    const barW = WIDTH / BUCKETS;
    return counts.map((c, i) => {
      const h = (c / peak) * HEIGHT;
      return (
        <rect
          key={i}
          x={i * barW}
          y={HEIGHT - h}
          width={Math.max(0, barW - 1)}
          height={h}
          className="fill-fg-muted/70"
        />
      );
    });
  }, [data]);

  if (!bars) return null;
  return (
    <svg
      width={WIDTH}
      height={HEIGHT}
      viewBox={`0 0 ${WIDTH} ${HEIGHT}`}
      role="img"
      aria-label={`${sessionName} tool call frequency, last 10 min`}
      className="shrink-0"
    >
      {bars}
    </svg>
  );
}
