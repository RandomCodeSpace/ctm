package ingest_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/ingest"
	"github.com/RandomCodeSpace/ctm/internal/session"
)

// fakeTmux records call counts per session name and returns a canned
// alive value. Safe for concurrent use.
type fakeTmux struct {
	mu    sync.Mutex
	calls map[string]int
	alive map[string]bool
}

func newFakeTmux() *fakeTmux {
	return &fakeTmux{calls: map[string]int{}, alive: map[string]bool{}}
}

func (f *fakeTmux) HasSession(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[name]++
	return f.alive[name]
}

func (f *fakeTmux) callCount(name string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[name]
}

func (f *fakeTmux) setAlive(name string, alive bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alive[name] = alive
}

// writeSessionsFile serializes sessions in the same shape as
// session.Store.save (schema_version + sessions map keyed by name) but
// without going through the locked Store API.
func writeSessionsFile(t *testing.T, path string, sessions ...*session.Session) {
	t.Helper()
	m := make(map[string]*session.Session, len(sessions))
	for _, s := range sessions {
		m[s.Name] = s
	}
	body := map[string]any{
		"schema_version": session.SchemaVersion,
		"sessions":       m,
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal sessions fixture: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write sessions fixture: %v", err)
	}
}

func TestProjection_InitialLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	s := session.New("alpha", "/work/alpha", "safe")
	writeSessionsFile(t, path, s)

	p := ingest.New(path, newFakeTmux())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	done := make(chan struct{})
	go func() { _ = p.Run(ctx); close(done) }()

	if !waitFor(t, 2*time.Second, func() bool {
		_, ok := p.Get("alpha")
		return ok
	}) {
		t.Fatal("projection never saw alpha after initial load")
	}

	all := p.All()
	if len(all) != 1 || all[0].Name != "alpha" {
		t.Fatalf("All() = %+v, want one [alpha]", all)
	}
	if all[0].UUID != s.UUID {
		t.Errorf("UUID round-trip mismatch: got %q want %q", all[0].UUID, s.UUID)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
}

func TestProjection_PicksUpRewrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	writeSessionsFile(t, path, session.New("alpha", "/work/alpha", "safe"))

	// Force the first file's mtime into the past so the second write's
	// mtime is guaranteed to be strictly greater on filesystems with
	// 1 s mtime granularity.
	past := time.Now().Add(-2 * time.Second)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	p := ingest.New(path, newFakeTmux())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = p.Run(ctx) }()

	if !waitFor(t, 2*time.Second, func() bool {
		_, ok := p.Get("alpha")
		return ok
	}) {
		t.Fatal("projection never saw alpha")
	}

	// Second write — add beta, drop alpha.
	writeSessionsFile(t, path, session.New("beta", "/work/beta", "yolo"))

	if !waitFor(t, 3*time.Second, func() bool {
		_, gotBeta := p.Get("beta")
		_, gotAlpha := p.Get("alpha")
		return gotBeta && !gotAlpha
	}) {
		t.Fatalf("projection did not re-read after rewrite; All=%v", p.All())
	}
}

func TestProjection_LenientDecode_UnknownFields(t *testing.T) {
	// Unknown top-level fields and unknown per-session fields must not
	// break the projection (serve is a consumer; CLI owns strictness).
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")

	body := []byte(`{
		"schema_version": 1,
		"future_top_level_field": "ignored",
		"sessions": {
			"alpha": {
				"name": "alpha",
				"uuid": "u-alpha",
				"mode": "safe",
				"workdir": "/work/alpha",
				"created_at": "2026-04-20T12:34:56Z",
				"future_per_session_field": {"x": 1}
			}
		}
	}`)
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := ingest.New(path, newFakeTmux())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = p.Run(ctx) }()

	if !waitFor(t, 2*time.Second, func() bool {
		s, ok := p.Get("alpha")
		return ok && s.UUID == "u-alpha"
	}) {
		t.Fatalf("lenient decode failed; All=%v", p.All())
	}
}

func TestProjection_TmuxAliveTTLCache(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	writeSessionsFile(t, path, session.New("alpha", "/work/alpha", "safe"))

	tx := newFakeTmux()
	tx.setAlive("alpha", true)

	p := ingest.New(path, tx)

	// Inject a controllable clock. Use atomic.Pointer so the test can
	// fast-forward without racing with TmuxAlive's reads.
	var clock atomic.Pointer[time.Time]
	now := time.Unix(1_700_000_000, 0)
	clock.Store(&now)
	ingest.SetClockForTest(p, func() time.Time { return *clock.Load() })

	// First call → upstream probe.
	if !p.TmuxAlive("alpha") {
		t.Fatal("expected alpha to be alive on first probe")
	}
	if got := tx.callCount("alpha"); got != 1 {
		t.Fatalf("expected 1 upstream call, got %d", got)
	}

	// Second call within TTL → cached, no new probe.
	advance := now.Add(2 * time.Second)
	clock.Store(&advance)
	if !p.TmuxAlive("alpha") {
		t.Fatal("expected cached alive=true")
	}
	if got := tx.callCount("alpha"); got != 1 {
		t.Fatalf("expected still 1 upstream call after cache hit, got %d", got)
	}

	// Fast-forward beyond TTL → fresh probe.
	advance = now.Add(6 * time.Second)
	clock.Store(&advance)
	if !p.TmuxAlive("alpha") {
		t.Fatal("expected alive on refresh")
	}
	if got := tx.callCount("alpha"); got != 2 {
		t.Fatalf("expected 2 upstream calls after TTL expiry, got %d", got)
	}
}

// waitFor polls cond up to d, sleeping briefly between checks. Returns
// true if cond became true. Used to bridge between Run's polling and
// the test's synchronous assertions without flaky fixed sleeps.
func waitFor(t *testing.T, d time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}
