package hermes

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func shimPath(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs("testdata/fake-hermes.sh")
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	return abs
}

func withShim(t *testing.T, fixture string) {
	t.Helper()
	t.Setenv("CTM_HERMES_BIN", shimPath(t))
	if fixture != "" {
		path := filepath.Join(t.TempDir(), "fixture.txt")
		if err := os.WriteFile(path, []byte(fixture), 0o644); err != nil {
			t.Fatalf("WriteFile fixture: %v", err)
		}
		t.Setenv("CTM_FAKE_HERMES_FIXTURE", path)
	}
	prevBudget, prevPoll := discoverBudgetVar, discoverPollVar
	discoverBudgetVar = 300 * time.Millisecond
	discoverPollVar = 50 * time.Millisecond
	t.Cleanup(func() {
		discoverBudgetVar = prevBudget
		discoverPollVar = prevPoll
	})
}

func TestDiscoverSessionID_match(t *testing.T) {
	fixture := "Preview         Last Active   Src    ID\n" +
		"─────────────────────────────────────────────────────\n" +
		"say hi          just now      cli    20260515_152727_9da209\n"
	withShim(t, fixture)
	spawnStart, _ := time.ParseInLocation("20060102_150405", "20260515_152700", time.Local)
	id, ok := DiscoverSessionID(spawnStart)
	if !ok || id != "20260515_152727_9da209" {
		t.Errorf("DiscoverSessionID = (%q, %v), want (%q, true)",
			id, ok, "20260515_152727_9da209")
	}
}

func TestDiscoverSessionID_newestWins(t *testing.T) {
	fixture := "Preview   Last Active   Src    ID\n" +
		"─────────────────────────────────────────────────\n" +
		"older     2m ago        cli    20260515_152700_aaaaaa\n" +
		"newer     just now      cli    20260515_153000_bbbbbb\n" +
		"oldest    5m ago        cli    20260515_152500_cccccc\n"
	withShim(t, fixture)
	spawnStart, _ := time.ParseInLocation("20060102_150405", "20260515_152600", time.Local)
	id, ok := DiscoverSessionID(spawnStart)
	if !ok || id != "20260515_153000_bbbbbb" {
		t.Errorf("DiscoverSessionID = (%q, %v), want newest (20260515_153000_bbbbbb, true)", id, ok)
	}
}

func TestDiscoverSessionID_cutoffFiltersOldRows(t *testing.T) {
	fixture := "Preview   Last Active   Src    ID\n" +
		"─────────────────────────────────────────────────\n" +
		"old       2m ago        cli    20260515_152700_aaaaaa\n"
	withShim(t, fixture)
	spawnStart, _ := time.ParseInLocation("20060102_150405", "20260515_153000", time.Local)
	id, ok := DiscoverSessionID(spawnStart)
	if ok || id != "" {
		t.Errorf("DiscoverSessionID = (%q, %v), want (\"\", false) for pre-cutoff row", id, ok)
	}
}

func TestDiscoverSessionID_emptyOutput(t *testing.T) {
	withShim(t, "")
	id, ok := DiscoverSessionID(time.Now())
	if ok || id != "" {
		t.Errorf("DiscoverSessionID = (%q, %v), want (\"\", false) for empty output", id, ok)
	}
}

func TestDiscoverSessionID_malformedLines(t *testing.T) {
	fixture := "not a real table\nnot even close\nrandom gibberish\n"
	withShim(t, fixture)
	id, ok := DiscoverSessionID(time.Now().Add(-1 * time.Hour))
	if ok || id != "" {
		t.Errorf("DiscoverSessionID = (%q, %v), want (\"\", false) for no IDs", id, ok)
	}
}

func TestDiscoverSessionID_missingBinaryReturnsFalse(t *testing.T) {
	t.Setenv("CTM_HERMES_BIN", "/nonexistent/path/to/hermes-xyz")
	prevBudget, prevPoll := discoverBudgetVar, discoverPollVar
	discoverBudgetVar = 200 * time.Millisecond
	discoverPollVar = 50 * time.Millisecond
	defer func() {
		discoverBudgetVar = prevBudget
		discoverPollVar = prevPoll
	}()

	id, ok := DiscoverSessionID(time.Now())
	if ok || id != "" {
		t.Errorf("DiscoverSessionID = (%q, %v), want (\"\", false)", id, ok)
	}
}

func TestHermesBin_defaultsToHermes(t *testing.T) {
	os.Unsetenv("CTM_HERMES_BIN")
	if got := hermesBin(); got != "hermes" {
		t.Errorf("hermesBin() = %q, want %q", got, "hermes")
	}
}

func TestHermesBin_envOverride(t *testing.T) {
	t.Setenv("CTM_HERMES_BIN", "/some/path")
	if got := hermesBin(); got != "/some/path" {
		t.Errorf("hermesBin() = %q, want %q", got, "/some/path")
	}
}
