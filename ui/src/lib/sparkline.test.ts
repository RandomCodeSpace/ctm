import { describe, expect, it } from "vitest";
import { bucketize } from "./sparkline";

const opts = { now: 1_000_000, windowMs: 10_000, buckets: 10 };

describe("bucketize", () => {
  it("returns a zero-filled array for empty input", () => {
    expect(bucketize([], opts)).toEqual([0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);
  });

  it("counts events in the right buckets (oldest-first)", () => {
    const oldest = opts.now - opts.windowMs;
    const ts = [oldest + 500, oldest + 1_500, oldest + 9_500];
    // bucketMs = 1000 → idx 0, 1, 9
    expect(bucketize(ts, opts)).toEqual([1, 1, 0, 0, 0, 0, 0, 0, 0, 1]);
  });

  it("drops events outside the window", () => {
    expect(
      bucketize([opts.now - opts.windowMs - 1, opts.now + 1], opts),
    ).toEqual([0, 0, 0, 0, 0, 0, 0, 0, 0, 0]);
  });

  it("clamps an event at now into the last bucket", () => {
    const out = bucketize([opts.now], opts);
    expect(out[9]).toBe(1);
  });

  it("returns empty for non-positive buckets or windowMs", () => {
    expect(bucketize([1, 2], { now: 0, windowMs: 0, buckets: 5 })).toEqual([]);
    expect(bucketize([1, 2], { now: 0, windowMs: 10, buckets: 0 })).toEqual([]);
  });
});
