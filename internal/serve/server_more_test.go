package serve

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/api"
	"github.com/RandomCodeSpace/ctm/internal/serve/ingest"
	"github.com/RandomCodeSpace/ctm/internal/serve/store"
	"github.com/RandomCodeSpace/ctm/internal/session"
)

// fakeCostStore is a tiny store.CostStore stub whose Range/Totals
// return a fixed error. Used to drive costSourceAdapter's err
// branches. Insert/Close are no-ops for the few tests that touch
// them (none here, but they're satisfied by interface).
type fakeCostStore struct {
	rangeErr  error
	totalsErr error
	rangePts  []store.Point
	totals    store.Totals
}

func (f *fakeCostStore) Insert([]store.Point) error { return nil }
func (f *fakeCostStore) Range(session string, since, until time.Time) ([]store.Point, error) {
	if f.rangeErr != nil {
		return nil, f.rangeErr
	}
	return f.rangePts, nil
}
func (f *fakeCostStore) Totals(since time.Time) (store.Totals, error) {
	if f.totalsErr != nil {
		return store.Totals{}, f.totalsErr
	}
	return f.totals, nil
}
func (f *fakeCostStore) Close() error { return nil }

// TestCostSourceAdapter_RangeError covers the "Range returns err"
// fast-fail branch in costSourceAdapter.Range.
func TestCostSourceAdapter_RangeError(t *testing.T) {
	wantErr := errors.New("boom")
	a := costSourceAdapter{s: &fakeCostStore{rangeErr: wantErr}}
	pts, err := a.Range("alpha", time.Now().Add(-time.Hour), time.Now())
	if err == nil || !errors.Is(err, wantErr) {
		t.Errorf("Range err = %v, want %v", err, wantErr)
	}
	if pts != nil {
		t.Errorf("Range pts = %v, want nil", pts)
	}
}

// TestCostSourceAdapter_TotalsError covers the "Totals returns err"
// fast-fail branch.
func TestCostSourceAdapter_TotalsError(t *testing.T) {
	wantErr := errors.New("boom")
	a := costSourceAdapter{s: &fakeCostStore{totalsErr: wantErr}}
	got, err := a.Totals(time.Now().Add(-time.Hour))
	if err == nil || !errors.Is(err, wantErr) {
		t.Errorf("Totals err = %v, want %v", err, wantErr)
	}
	if got != (api.CostTotals{}) {
		t.Errorf("Totals = %+v, want zero", got)
	}
}

// TestLogsUUIDResolver_NilProjOrEmptyArgs covers the early-return
// guards on both Resolve methods.
func TestLogsUUIDResolver_NilProjOrEmptyArgs(t *testing.T) {
	// nil projection.
	r := logsUUIDResolver{proj: nil}
	if _, ok := r.ResolveName("alpha"); ok {
		t.Errorf("ResolveName(nil proj) should return false")
	}
	if _, ok := r.ResolveUUID("uuid"); ok {
		t.Errorf("ResolveUUID(nil proj) should return false")
	}

	// empty projection but non-empty proj — empty arg short-circuits.
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	writeSessionsJSON(t, path)
	proj := ingest.New(path, &fakeTmuxClient{})
	proj.Reload()
	r2 := logsUUIDResolver{proj: proj}
	if _, ok := r2.ResolveName(""); ok {
		t.Errorf("ResolveName(\"\") should return false")
	}
	if _, ok := r2.ResolveUUID(""); ok {
		t.Errorf("ResolveUUID(\"\") should return false")
	}
}

// TestLogsUUIDResolver_ResolveName_NoUUIDStored covers the
// "session exists but UUID empty" branch.
func TestLogsUUIDResolver_ResolveName_NoUUIDStored(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	// Session has no UUID set.
	writeSessionsJSON(t, path, &session.Session{Name: "alpha", Mode: "safe"})
	proj := ingest.New(path, &fakeTmuxClient{})
	proj.Reload()

	r := logsUUIDResolver{proj: proj}
	if _, ok := r.ResolveName("alpha"); ok {
		t.Errorf("ResolveName for empty-UUID session should return false")
	}
}

// TestLogsUUIDResolver_ResolveUUID_WorkdirFallbackHit covers the
// production workdir-derived fallback: when the requested uuid isn't
// in the projection's direct uuid map, ResolveUUID globs
// ~/.claude/projects/<dir>/<uuid>.jsonl, derives the session from the
// dirname (slashes → dashes), and returns its name.
func TestLogsUUIDResolver_ResolveUUID_WorkdirFallbackHit(t *testing.T) {
	// Sandbox HOME so UserHomeDir + Glob hit our fake tree only.
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	// On Linux UserHomeDir consults $HOME, on macOS too; this is enough
	// for CI and dev. Skip if UserHomeDir surprises us.
	if got, err := os.UserHomeDir(); err != nil || got != homeDir {
		t.Skipf("UserHomeDir didn't honour HOME override: got=%q err=%v", got, err)
	}

	// Real workdir for the session.
	workdir := "/srv/projects/codeiq"
	// Claude projects layout: ~/.claude/projects/<workdir-with-slashes-as-dashes>/<uuid>.jsonl
	dirName := strings.ReplaceAll(workdir, "/", "-")
	projDir := filepath.Join(homeDir, ".claude", "projects", dirName)
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	const uuid = "ffffffff-0000-0000-0000-000000000001"
	if err := os.WriteFile(filepath.Join(projDir, uuid+".jsonl"), []byte{}, 0o600); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	// Projection has the session WITHOUT this uuid (e.g. session
	// rotated to a fresh claude session_id but the old transcript
	// still exists on disk). The direct loop misses, the workdir
	// fallback should still resolve to "codeiq" via the dirName.
	dir := t.TempDir()
	sessionsPath := filepath.Join(dir, "sessions.json")
	writeSessionsJSON(t, sessionsPath, &session.Session{
		Name:    "codeiq",
		UUID:    "current-uuid-different",
		Workdir: workdir,
	})
	proj := ingest.New(sessionsPath, &fakeTmuxClient{})
	proj.Reload()

	r := logsUUIDResolver{proj: proj}
	got, ok := r.ResolveUUID(uuid)
	if !ok {
		t.Fatalf("ResolveUUID(orphan uuid via workdir fallback) returned false")
	}
	if got != "codeiq" {
		t.Errorf("ResolveUUID = %q, want codeiq", got)
	}
}

// TestLogsUUIDResolver_ResolveUUID_FallbackHitButNoSessionMatch covers
// the case where the glob matches a single file but no projection
// session has a workdir whose dashed form equals the dirname → false.
func TestLogsUUIDResolver_ResolveUUID_FallbackHitButNoSessionMatch(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	if got, err := os.UserHomeDir(); err != nil || got != homeDir {
		t.Skipf("UserHomeDir didn't honour HOME override: got=%q err=%v", got, err)
	}

	dirName := "-srv-projects-codeiq"
	projDir := filepath.Join(homeDir, ".claude", "projects", dirName)
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	const uuid = "fffffffe-0000-0000-0000-000000000001"
	if err := os.WriteFile(filepath.Join(projDir, uuid+".jsonl"), []byte{}, 0o600); err != nil {
		t.Fatalf("write jsonl: %v", err)
	}

	// Projection has a session but its workdir doesn't match the dashed
	// form ("/totally/different").
	dir := t.TempDir()
	sessionsPath := filepath.Join(dir, "sessions.json")
	writeSessionsJSON(t, sessionsPath, &session.Session{
		Name:    "alpha",
		UUID:    "u-alpha",
		Workdir: "/totally/different",
	})
	proj := ingest.New(sessionsPath, &fakeTmuxClient{})
	proj.Reload()

	r := logsUUIDResolver{proj: proj}
	if got, ok := r.ResolveUUID(uuid); ok {
		t.Errorf("ResolveUUID = (%q, true), want false (no workdir match)", got)
	}
}

// TestSessionSourceAdapter_LastCheckpointAt_RealRepo covers the
// "cps[0].TS parses cleanly → return non-zero time" success branch
// of LastCheckpointAt. Requires git in PATH; skipped if absent.
func TestSessionSourceAdapter_LastCheckpointAt_RealRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not in PATH: %v", err)
	}

	// Build a real git repo with one checkpoint commit so
	// CheckpointsCache.Get returns a populated slice with an RFC3339 TS.
	repoDir := t.TempDir()
	gitInit(t, repoDir)
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	gitRun(t, repoDir, "add", ".")
	gitRun(t, repoDir, "commit", "-m", "checkpoint: pre-yolo 2026-04-21T12:00:00")

	// Wire a projection with this workdir and a session source adapter.
	sessionsPath := filepath.Join(t.TempDir(), "sessions.json")
	writeSessionsJSON(t, sessionsPath, &session.Session{
		Name: "alpha", UUID: "u-alpha", Workdir: repoDir,
	})
	proj := ingest.New(sessionsPath, &fakeTmuxClient{})
	proj.Reload()

	a := sessionSourceAdapter{proj: proj, cpCache: api.NewCheckpointsCache()}
	ts, ok := a.LastCheckpointAt("alpha")
	if !ok {
		t.Fatalf("LastCheckpointAt returned ok=false on real checkpointed repo")
	}
	if ts.IsZero() {
		t.Errorf("LastCheckpointAt ts is zero, want non-zero")
	}
}

// gitInit / gitRun are minimal shell-out helpers for the repo fixture.
// They mirror the helpers in internal/serve/git/checkpoints_test.go.
func gitInit(t *testing.T, dir string) {
	t.Helper()
	gitRun(t, dir, "init", "-q")
	gitRun(t, dir, "config", "user.email", "test@example.com")
	gitRun(t, dir, "config", "user.name", "Test")
	// Some hosts default to gpgsign=true; force off so commits don't
	// hang waiting for a signing agent we don't have.
	gitRun(t, dir, "config", "commit.gpgsign", "false")
}

// TestNew_DefaultsAllUnset covers the four `if path == ""` branches in
// New() that fall through to config.SessionsPath / TmuxConfPath /
// Dir()/logs / /tmp/ctm-statusline. Sandboxes HOME so the cost db is
// written into a temp tree rather than the dev's real ~/.config/ctm.
func TestNew_DefaultsAllUnset(t *testing.T) {
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	if got, err := os.UserHomeDir(); err != nil || got != homeDir {
		t.Skipf("UserHomeDir didn't honour HOME override: got=%q err=%v", got, err)
	}
	// config.Dir uses UserHomeDir → ~/.config/ctm. Pre-create so
	// OpenCostStore doesn't trip on the parent.
	if err := os.MkdirAll(filepath.Join(homeDir, ".config", "ctm"), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	port := pickFreePort(t)
	srv, err := New(Options{
		Port:    port,
		Version: "vDef",
		Token:   testToken,
		// Intentionally leave SessionsPath / TmuxConfPath / LogDir /
		// StatuslineDumpDir empty — the empty-path branches inside
		// New() should fall through to config defaults.
	})
	if err != nil {
		t.Fatalf("New defaults: %v", err)
	}
	t.Cleanup(func() { _ = srv.listener.Close(); _ = srv.cost.Close() })

	if !strings.HasPrefix(srv.logDir, homeDir) {
		t.Errorf("logDir = %q, want under %q", srv.logDir, homeDir)
	}
}

// TestNew_PortInUseByNonCtm covers the "port bound by foreign listener"
// branch (lines 170-176). We bind a vanilla TCP listener first, then
// call New() against the same port — it should fail with the bind
// error wrapped, NOT ErrAlreadyRunning (probeIsCtmServe sees a non-ctm
// listener).
func TestNew_PortInUseByNonCtm(t *testing.T) {
	port := pickFreePort(t)
	// Bind a TCP listener that doesn't speak ctm-serve's healthz.
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("bind decoy: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	_, err = New(Options{
		Port:              port,
		Version:           "vClash",
		Token:             testToken,
		SessionsPath:      filepath.Join(t.TempDir(), "sessions.json"),
		TmuxConfPath:      filepath.Join(t.TempDir(), "tmux.conf"),
		LogDir:            filepath.Join(t.TempDir(), "logs"),
		StatuslineDumpDir: filepath.Join(t.TempDir(), "statusline"),
	})
	if err == nil {
		t.Fatal("New on busy port returned nil error, want bind failure")
	}
	if errors.Is(err, ErrAlreadyRunning) {
		t.Errorf("err = ErrAlreadyRunning; want non-ctm bind-failure wrap. got=%v", err)
	}
}

// TestEventsSessionRoute exercises the GET /events/session/{name}
// closure registered in registerRoutes — the line counted at 779. We
// can't keep the SSE connection open for long, but a 200 + initial
// retry frame is enough to record coverage on the route.
func TestEventsSessionRoute(t *testing.T) {
	port := pickFreePort(t)
	srv, err := New(Options{
		Port:              port,
		Version:           "vSSE",
		Token:             testToken,
		SessionsPath:      filepath.Join(t.TempDir(), "sessions.json"),
		TmuxConfPath:      filepath.Join(t.TempDir(), "tmux.conf"),
		LogDir:            filepath.Join(t.TempDir(), "logs"),
		StatuslineDumpDir: filepath.Join(t.TempDir(), "statusline"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = srv.listener.Close(); _ = srv.cost.Close() })

	mux := http.NewServeMux()
	srv.registerRoutes(mux)

	// httptest.NewRecorder doesn't support Flush detection, but for SSE
	// we just want the handler entered and response headers written.
	// Use a real test server so the underlying Hijack/Flush works.
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/events/session/alpha", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)

	// Use a client with an aggressive timeout so the long-lived SSE
	// loop returns control to the test promptly.
	client := &http.Client{Timeout: 200 * time.Millisecond}
	resp, err := client.Do(req)
	if err != nil {
		// A timeout-on-read is fine — the route was hit. Surface only
		// non-timeout failures.
		if !strings.Contains(err.Error(), "Timeout") &&
			!strings.Contains(err.Error(), "deadline exceeded") &&
			!strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("Do: %v", err)
		}
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	// Force a fully self-contained env so user-level git config (eg.
	// commit signing, hooks) can't leak in from the dev's machine.
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
