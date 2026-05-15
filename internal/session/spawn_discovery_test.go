package session_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/RandomCodeSpace/ctm/internal/agent/codex" // register codex
	"github.com/RandomCodeSpace/ctm/internal/session"
)

// realStoreFakeTmux composes a real *session.Store (so the discovery
// path can stamp via UpdateAgentSessionID) with a fake tmux that
// records but does not exec. Returns the store + temp dir for HOME.
func realStoreFakeTmux(t *testing.T) (*session.Store, *fakeTmux, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	storePath := filepath.Join(home, "sessions.json")
	return session.NewStore(storePath), &fakeTmux{}, home
}

// dropRolloutAfter creates a codex-shaped rollout file in
// ~/.codex/sessions/<today>/ after delay. Returns the UUID that ends
// up in the filename so the test can assert against it.
func dropRolloutAfter(t *testing.T, home string, delay time.Duration) string {
	t.Helper()
	uuid := "019dd200-1111-7000-8000-aaaabbbbcccc"
	now := time.Now().UTC()
	day := filepath.Join(home, ".codex", "sessions",
		now.Format("2006"), now.Format("01"), now.Format("02"))
	if err := os.MkdirAll(day, 0755); err != nil {
		t.Fatalf("mkdir codex day-dir: %v", err)
	}
	go func() {
		time.Sleep(delay)
		name := "rollout-" + now.Format("2006-01-02T15-04-05") + "-" + uuid + ".jsonl"
		path := filepath.Join(day, name)
		_ = os.WriteFile(path, []byte(`{"type":"session_meta"}`), 0644)
	}()
	return uuid
}

// TestYolo_DiscoveryStampsAgentSessionID verifies the full end-to-end
// flow: session.Yolo spawns, fires the discovery goroutine, the
// goroutine finds the (simulated) codex rollout file, and stamps the
// session row's AgentSessionID. Uses OnDiscoveryComplete to
// synchronize without depending on flaky time-based sleeps.
func TestYolo_DiscoveryStampsAgentSessionID(t *testing.T) {
	store, tmux, home := realStoreFakeTmux(t)

	// Set up: simulate codex writing its rollout file ~50ms after spawn.
	wantUUID := dropRolloutAfter(t, home, 50*time.Millisecond)

	done := make(chan struct{})
	wd := t.TempDir() // any real dir works as workdir

	sess, err := session.Yolo(session.SpawnOpts{
		Name:                "discsess",
		Workdir:             wd,
		Tmux:                tmux,
		Store:               store,
		OnDiscoveryComplete: func() { close(done) },
	})
	if err != nil {
		t.Fatalf("Yolo: %v", err)
	}
	if sess.AgentSessionID != "" {
		t.Errorf("Yolo returned AgentSessionID=%q before discovery; expected empty (stamp happens async)", sess.AgentSessionID)
	}

	select {
	case <-done:
	case <-time.After(6 * time.Second):
		t.Fatal("discovery goroutine did not complete within 6s")
	}

	// After discovery completes, store row must carry the UUID.
	got, err := store.Get("discsess")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AgentSessionID != wantUUID {
		t.Fatalf("AgentSessionID = %q, want %q", got.AgentSessionID, wantUUID)
	}
	if tmux.newCalled != 1 {
		t.Errorf("tmux.NewSession called %d times, want 1", tmux.newCalled)
	}
}

// TestYolo_DiscoveryTimeoutLeavesStoreRowEmpty verifies the
// codex-down/no-rollout path: discovery times out, OnDiscoveryComplete
// still fires, and the store row's AgentSessionID stays empty so
// `codex resume --last` semantics keep working.
func TestYolo_DiscoveryTimeoutLeavesStoreRowEmpty(t *testing.T) {
	// Shrink the discovery budget so the test doesn't take 5s.
	t.Setenv("HOME", t.TempDir())
	// Note: we deliberately don't create ~/.codex/sessions/ so
	// scanForRollout finds nothing each poll.
	store := session.NewStore(filepath.Join(os.Getenv("HOME"), "sessions.json"))
	tmux := &fakeTmux{}

	// We rely on the test budget being short enough to finish within
	// the test's wait. The production constant is 5s; for this test
	// the timeout will happen naturally before our 6s wait.
	done := make(chan struct{})
	wd := t.TempDir()

	if _, err := session.Yolo(session.SpawnOpts{
		Name:                "timeoutsess",
		Workdir:             wd,
		Tmux:                tmux,
		Store:               store,
		OnDiscoveryComplete: func() { close(done) },
	}); err != nil {
		t.Fatalf("Yolo: %v", err)
	}

	select {
	case <-done:
	case <-time.After(7 * time.Second):
		t.Fatal("discovery goroutine did not complete within 7s")
	}

	got, err := store.Get("timeoutsess")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AgentSessionID != "" {
		t.Fatalf("expected empty AgentSessionID on timeout, got %q", got.AgentSessionID)
	}
}
