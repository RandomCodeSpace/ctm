package codex

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeCodexHome wires HOME to a temp dir and pre-creates the
// ~/.codex/sessions/<spawnDate> directory tree so the test can drop
// rollout fixtures in. Returns the day-directory path the test
// writes into.
func fakeCodexHome(t *testing.T, spawn time.Time) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	day := filepath.Join(home, ".codex", "sessions",
		spawn.UTC().Format("2006"),
		spawn.UTC().Format("01"),
		spawn.UTC().Format("02"))
	if err := os.MkdirAll(day, 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	return day
}

func writeRollout(t *testing.T, dir, name string, mtime time.Time) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(`{"type":"session_meta"}`), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
}

// shrinkBudget swaps the discovery timing constants to test-friendly
// values for the duration of t. Without this, a "no match" case takes
// the full 5-second production budget.
func shrinkBudget(t *testing.T) {
	t.Helper()
	origBudget := discoverBudgetVar
	origPoll := discoverPollVar
	discoverBudgetVar = 200 * time.Millisecond
	discoverPollVar = 25 * time.Millisecond
	t.Cleanup(func() {
		discoverBudgetVar = origBudget
		discoverPollVar = origPoll
	})
}

func TestDiscoverSessionID_FindsRolloutAfterSpawn(t *testing.T) {
	shrinkBudget(t)
	spawn := time.Now()
	day := fakeCodexHome(t, spawn)
	// File mtime = spawn + 50ms — first poll iteration should pick it up.
	writeRollout(t, day,
		"rollout-2026-05-14T12-00-00-019dd15a-458b-7341-80de-7d67e796f06f.jsonl",
		spawn.Add(50*time.Millisecond))

	id, ok := DiscoverSessionID(spawn)
	if !ok {
		t.Fatal("expected discovery to succeed")
	}
	want := "019dd15a-458b-7341-80de-7d67e796f06f"
	if id != want {
		t.Fatalf("id = %q, want %q", id, want)
	}
}

func TestDiscoverSessionID_IgnoresPreSpawnFiles(t *testing.T) {
	shrinkBudget(t)
	spawn := time.Now()
	day := fakeCodexHome(t, spawn)
	// File written WAY before spawn — must not match.
	writeRollout(t, day,
		"rollout-2026-05-14T11-00-00-019dd000-0000-0000-0000-000000000000.jsonl",
		spawn.Add(-1*time.Hour))

	if id, ok := DiscoverSessionID(spawn); ok {
		t.Fatalf("expected timeout, got id=%q ok=%v", id, ok)
	}
}

func TestDiscoverSessionID_PicksNewestWhenMultiple(t *testing.T) {
	shrinkBudget(t)
	spawn := time.Now()
	day := fakeCodexHome(t, spawn)
	writeRollout(t, day,
		"rollout-2026-05-14T12-00-00-019dd111-0000-0000-0000-000000000000.jsonl",
		spawn.Add(50*time.Millisecond))
	writeRollout(t, day,
		"rollout-2026-05-14T12-00-01-019dd222-0000-0000-0000-000000000000.jsonl",
		spawn.Add(75*time.Millisecond)) // newest, wins
	writeRollout(t, day,
		"rollout-2026-05-14T12-00-02-019dd333-0000-0000-0000-000000000000.jsonl",
		spawn.Add(60*time.Millisecond))

	id, ok := DiscoverSessionID(spawn)
	if !ok {
		t.Fatal("expected discovery")
	}
	if id != "019dd222-0000-0000-0000-000000000000" {
		t.Fatalf("id = %q, want newest (019dd222-…)", id)
	}
}

func TestDiscoverSessionID_TimeoutWhenSessionsDirAbsent(t *testing.T) {
	shrinkBudget(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	// No ~/.codex/sessions tree at all.
	if id, ok := DiscoverSessionID(time.Now()); ok {
		t.Fatalf("expected timeout on missing dir, got id=%q ok=%v", id, ok)
	}
}

func TestDiscoverSessionID_SkipsNonRolloutFiles(t *testing.T) {
	shrinkBudget(t)
	spawn := time.Now()
	day := fakeCodexHome(t, spawn)
	// A file that is fresh but doesn't match the rollout name shape.
	writeRollout(t, day, "scratch.jsonl", spawn.Add(50*time.Millisecond))
	// And one with a rollout prefix but missing the UUID suffix shape.
	writeRollout(t, day, "rollout-2026-05-14T12-00-00.jsonl", spawn.Add(50*time.Millisecond))

	if id, ok := DiscoverSessionID(spawn); ok {
		t.Fatalf("expected no match, got id=%q ok=%v", id, ok)
	}
}

func TestRolloutFilenameRe_HappyPath(t *testing.T) {
	name := "rollout-2026-04-27T23-50-47-019dd15a-458b-7341-80de-7d67e796f06f.jsonl"
	m := rolloutFilenameRe.FindStringSubmatch(name)
	if len(m) != 2 {
		t.Fatalf("regex did not capture: %v", m)
	}
	if m[1] != "019dd15a-458b-7341-80de-7d67e796f06f" {
		t.Fatalf("capture = %q", m[1])
	}
}
