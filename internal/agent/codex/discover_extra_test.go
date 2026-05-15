package codex

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDayDirsFor_MidnightAddsPrevDay covers the clock-skew branch:
// when t falls in the first 5 minutes of a UTC day, dayDirsFor should
// also include the previous day's directory so a rollover-skewed file
// is still found.
func TestDayDirsFor_MidnightAddsPrevDay(t *testing.T) {
	root := "/codex/root"
	// 00:02 UTC on 2026-04-27 — well within the 5-minute window.
	t0 := time.Date(2026, 4, 27, 0, 2, 0, 0, time.UTC)
	dirs := dayDirsFor(root, t0)
	if len(dirs) != 2 {
		t.Fatalf("expected 2 dirs (today + previous), got %d: %v", len(dirs), dirs)
	}
	want0 := filepath.Join(root, "2026", "04", "27")
	want1 := filepath.Join(root, "2026", "04", "26")
	if dirs[0] != want0 {
		t.Errorf("dirs[0] = %q, want %q", dirs[0], want0)
	}
	if dirs[1] != want1 {
		t.Errorf("dirs[1] = %q, want %q", dirs[1], want1)
	}
}

// TestDayDirsFor_OutsideMidnightWindow covers the negative branch of
// the same conditional: at noon UTC only today's day-dir is returned.
func TestDayDirsFor_OutsideMidnightWindow(t *testing.T) {
	root := "/codex/root"
	t0 := time.Date(2026, 4, 27, 12, 0, 0, 0, time.UTC)
	dirs := dayDirsFor(root, t0)
	if len(dirs) != 1 {
		t.Fatalf("expected 1 dir, got %d: %v", len(dirs), dirs)
	}
}

// TestDayDirsFor_HourZeroPastFiveMinutes verifies the boundary: at
// 00:05 UTC the previous-day include is gone (the conditional is
// strict `< 5`).
func TestDayDirsFor_HourZeroPastFiveMinutes(t *testing.T) {
	root := "/codex/root"
	t0 := time.Date(2026, 4, 27, 0, 5, 0, 0, time.UTC)
	dirs := dayDirsFor(root, t0)
	if len(dirs) != 1 {
		t.Fatalf("expected 1 dir at 00:05, got %d: %v", len(dirs), dirs)
	}
}

// TestNewestMatchingRollout_DirectoryInDir covers the inner-loop
// `e.IsDir() continue` branch: a sub-directory in the day-dir is
// ignored even if its name superficially matches.
func TestNewestMatchingRollout_DirectoryInDir(t *testing.T) {
	spawn := time.Now()
	day := fakeCodexHome(t, spawn)
	// Drop a directory shaped like a rollout file — must be skipped.
	rolloutDir := filepath.Join(day, "rollout-2026-05-14T12-00-00-deadbeef.jsonl")
	if err := os.MkdirAll(rolloutDir, 0o755); err != nil {
		t.Fatal(err)
	}
	id, _, ok := newestMatchingRollout(day, spawn)
	if ok {
		t.Fatalf("expected no match (only a dir present), got id=%q", id)
	}
}

// TestNewestMatchingRollout_NonJsonlSkipped covers the
// "name doesn't end with .jsonl" branch in newestMatchingRollout.
func TestNewestMatchingRollout_NonJsonlSkipped(t *testing.T) {
	spawn := time.Now()
	day := fakeCodexHome(t, spawn)
	writeRollout(t, day, "rollout-2026-05-14T12-00-00-abc.txt", spawn.Add(50*time.Millisecond))
	id, _, ok := newestMatchingRollout(day, spawn)
	if ok {
		t.Fatalf("expected non-jsonl to be skipped, got id=%q", id)
	}
}

