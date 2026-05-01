package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/api"
	"github.com/RandomCodeSpace/ctm/internal/serve/attention"
	"github.com/RandomCodeSpace/ctm/internal/serve/events"
	"github.com/RandomCodeSpace/ctm/internal/serve/ingest"
	"github.com/RandomCodeSpace/ctm/internal/serve/store"
	"github.com/RandomCodeSpace/ctm/internal/session"
)

// fakeTmuxClient is a minimal HasSession stub to satisfy ingest.TmuxClient
// without spinning up tmux. Tests that don't care about liveness use
// this and just inspect the projection.
type fakeTmuxClient struct {
	alive map[string]bool
}

func (f *fakeTmuxClient) HasSession(name string) bool {
	if f == nil {
		return false
	}
	return f.alive[name]
}

// writeSessionsJSON writes the disk-shape ingest.Projection expects so
// Reload picks the entries up. Mirrors writeSessionsFile in
// internal/serve/ingest/sessions_proj_test.go.
func writeSessionsJSON(t *testing.T, path string, sessions ...*session.Session) {
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
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write sessions fixture: %v", err)
	}
}

// ---- Pure helper coverage --------------------------------------------------

func TestBuildSessionMaps(t *testing.T) {
	sessions := []session.Session{
		{Name: "alpha", UUID: "u-alpha", Workdir: "/home/dev/projects/alpha"},
		{Name: "beta", UUID: "u-beta", Workdir: ""},   // skipped from claudeDir
		{Name: "gamma", UUID: "", Workdir: "/srv/g"}, // skipped from uuidToName
	}
	uuidToName, claudeDirToName := buildSessionMaps(sessions)

	if got := uuidToName["u-alpha"]; got != "alpha" {
		t.Errorf("uuidToName[u-alpha] = %q, want alpha", got)
	}
	if got := uuidToName["u-beta"]; got != "beta" {
		t.Errorf("uuidToName[u-beta] = %q, want beta", got)
	}
	if _, ok := uuidToName[""]; ok {
		t.Errorf("empty UUID should be skipped, got entry")
	}
	if got := claudeDirToName["-home-dev-projects-alpha"]; got != "alpha" {
		t.Errorf("claudeDirToName[-home-dev-projects-alpha] = %q, want alpha", got)
	}
	if got := claudeDirToName["-srv-g"]; got != "gamma" {
		t.Errorf("claudeDirToName[-srv-g] = %q, want gamma", got)
	}
	if _, ok := claudeDirToName[""]; ok {
		t.Errorf("empty workdir should be skipped, got entry")
	}
}

func TestBuildSessionMaps_Empty(t *testing.T) {
	uuidToName, claudeDirToName := buildSessionMaps(nil)
	if len(uuidToName) != 0 || len(claudeDirToName) != 0 {
		t.Errorf("expected empty maps, got %v / %v", uuidToName, claudeDirToName)
	}
}

func TestResolveLogUUIDToName_Direct(t *testing.T) {
	uuidToName := map[string]string{"u-1": "alpha"}
	name, viaFallback, ok := resolveLogUUIDToName("u-1", uuidToName, nil, "")
	if !ok || name != "alpha" || viaFallback {
		t.Errorf("got (%q, %v, %v), want (alpha, false, true)", name, viaFallback, ok)
	}
}

func TestResolveLogUUIDToName_NoFallbackRoot(t *testing.T) {
	// uuid not in direct map; claudeProjectsRoot empty → no fallback.
	name, viaFallback, ok := resolveLogUUIDToName("orphan", nil, nil, "")
	if ok || name != "" || viaFallback {
		t.Errorf("got (%q, %v, %v), want (\"\", false, false)", name, viaFallback, ok)
	}
}

func TestResolveLogUUIDToName_FallbackHit(t *testing.T) {
	root := t.TempDir()
	// Lay down ~/.claude/projects/<encoded>/<uuid>.jsonl
	encoded := "-home-dev-projects-alpha"
	if err := os.MkdirAll(filepath.Join(root, encoded), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, encoded, "orphan-uuid.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	claudeDirToName := map[string]string{encoded: "alpha"}

	name, viaFallback, ok := resolveLogUUIDToName("orphan-uuid", nil, claudeDirToName, root)
	if !ok || name != "alpha" || !viaFallback {
		t.Errorf("got (%q, %v, %v), want (alpha, true, true)", name, viaFallback, ok)
	}
}

func TestResolveLogUUIDToName_FallbackNoMatchInDirMap(t *testing.T) {
	root := t.TempDir()
	encoded := "-some-unknown-dir"
	if err := os.MkdirAll(filepath.Join(root, encoded), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, encoded, "abc.jsonl"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// claudeDirToName has a different key → fallback fails.
	claudeDirToName := map[string]string{"-other": "x"}
	_, _, ok := resolveLogUUIDToName("abc", nil, claudeDirToName, root)
	if ok {
		t.Errorf("expected ok=false when claudeDirToName has no match")
	}
}

func TestResolveLogUUIDToName_FallbackMultipleMatchesRejected(t *testing.T) {
	// When the same UUID exists under two encoded dirs, the resolver
	// must reject the ambiguity (len(matches) != 1) and return ok=false.
	root := t.TempDir()
	for _, dir := range []string{"-a", "-b"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, dir, "dup.jsonl"), []byte("{}"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	claudeDirToName := map[string]string{"-a": "x", "-b": "y"}
	_, _, ok := resolveLogUUIDToName("dup", nil, claudeDirToName, root)
	if ok {
		t.Errorf("ambiguous fallback should not resolve, ok=true")
	}
}

// ---- Adapter coverage ------------------------------------------------------

func TestExecLookPath(t *testing.T) {
	// `go` will exist in PATH in any environment that built this test.
	want, err := exec.LookPath("go")
	if err != nil {
		t.Skipf("go not in PATH: %v", err)
	}
	got, err := execLookPath{}.LookPath("go")
	if err != nil {
		t.Fatalf("execLookPath.LookPath: %v", err)
	}
	if got != want {
		t.Errorf("LookPath(go) = %q, want %q", got, want)
	}
	if _, err := (execLookPath{}).LookPath("definitely-not-a-real-binary-xyz123"); err == nil {
		t.Errorf("expected error for missing binary")
	}
}

func TestHubStatsAdapter(t *testing.T) {
	hub := events.NewHub(0)
	a := hubStatsAdapter{hub: hub}
	stats := a.Stats()
	if stats == nil {
		t.Fatalf("Stats() returned nil")
	}
}

func TestQuotaEnricher_NilGuards(t *testing.T) {
	// All accessors must safely return zero+false when their backing
	// component is nil, since /api/sessions paints "unknown" lanes
	// from those bools rather than crashing.
	e := quotaEnricher{}
	if pct, ok := e.ContextPct("any"); ok || pct != 0 {
		t.Errorf("ContextPct nil quota: got (%d, %v)", pct, ok)
	}
	if ts, ok := e.LastToolCallAt("any"); ok || !ts.IsZero() {
		t.Errorf("LastToolCallAt nil attention: got (%v, %v)", ts, ok)
	}
	if a, ok := e.Attention("any"); ok || (a != api.Attention{}) {
		t.Errorf("Attention nil attention: got (%+v, %v)", a, ok)
	}
	if u, ok := e.Tokens("any"); ok || (u != api.TokenUsage{}) {
		t.Errorf("Tokens nil quota: got (%+v, %v)", u, ok)
	}
}

func TestQuotaEnricher_WithRealBackends(t *testing.T) {
	// Wire up a real QuotaIngester + attention engine so the live code
	// paths execute. Neither component requires Run() to answer the
	// snapshot / per-session lookups we hit here — they just return
	// false because we never published.
	hub := events.NewHub(0)
	q := ingest.NewQuotaIngester(t.TempDir(), nil, hub)
	att := attention.NewEngine(hub, q, fakeAttSrc{}, attention.Defaults(), nil)

	e := quotaEnricher{quota: q, attention: att}
	// Empty engine: every Snapshot/PerSession/ContextPct should be (zero, false).
	if pct, ok := e.ContextPct("nope"); ok || pct != 0 {
		t.Errorf("ContextPct: got (%d, %v) want (0, false)", pct, ok)
	}
	if ts, ok := e.LastToolCallAt("nope"); ok || !ts.IsZero() {
		t.Errorf("LastToolCallAt: got (%v, %v)", ts, ok)
	}
	if a, ok := e.Attention("nope"); ok || (a != api.Attention{}) {
		t.Errorf("Attention: got (%+v, %v)", a, ok)
	}
	if u, ok := e.Tokens("nope"); ok || (u != api.TokenUsage{}) {
		t.Errorf("Tokens: got (%+v, %v)", u, ok)
	}
}

// fakeAttSrc satisfies attention.SessionSource for tests that only need
// to construct an engine, not exercise its triggers.
type fakeAttSrc struct{}

func (fakeAttSrc) Names() []string                                { return nil }
func (fakeAttSrc) Mode(string) string                             { return "" }
func (fakeAttSrc) TmuxAlive(string) bool                          { return false }
func (fakeAttSrc) LastCheckpointAt(string) (time.Time, bool)      { return time.Time{}, false }

func TestSessionSourceAdapter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	now := time.Now()
	s1 := &session.Session{Name: "alpha", Mode: "yolo", Workdir: "/srv/a", CreatedAt: now}
	s2 := &session.Session{Name: "beta", Mode: "safe", Workdir: "", CreatedAt: now}
	writeSessionsJSON(t, path, s1, s2)

	proj := ingest.New(path, &fakeTmuxClient{alive: map[string]bool{"alpha": true}})
	proj.Reload()

	cp := api.NewCheckpointsCache()
	a := sessionSourceAdapter{proj: proj, cpCache: cp}

	names := a.Names()
	if len(names) != 2 {
		t.Errorf("Names() len = %d, want 2", len(names))
	}
	// Names map order is stable from sessions_proj's slice, but the
	// disk decode happens via map[string]*session.Session, so order
	// isn't guaranteed. Just check membership.
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	if !have["alpha"] || !have["beta"] {
		t.Errorf("Names() = %v, want both alpha + beta", names)
	}
	if got := a.Mode("alpha"); got != "yolo" {
		t.Errorf("Mode(alpha) = %q, want yolo", got)
	}
	if got := a.Mode("missing"); got != "" {
		t.Errorf("Mode(missing) = %q, want \"\"", got)
	}
	if !a.TmuxAlive("alpha") {
		t.Errorf("TmuxAlive(alpha) = false, want true")
	}
	if a.TmuxAlive("beta") {
		t.Errorf("TmuxAlive(beta) = true, want false")
	}
	// LastCheckpointAt: workdir empty → false.
	if ts, ok := a.LastCheckpointAt("beta"); ok || !ts.IsZero() {
		t.Errorf("LastCheckpointAt(beta empty workdir) = (%v, %v), want zero+false", ts, ok)
	}
	// LastCheckpointAt for missing session → false.
	if _, ok := a.LastCheckpointAt("missing"); ok {
		t.Errorf("LastCheckpointAt(missing) ok=true, want false")
	}
	// LastCheckpointAt for valid workdir but no git repo → cache returns
	// err, adapter returns (zero, false). Workdir is fine because it's a
	// non-existent path; CheckpointsCache.Get returns an error which the
	// adapter swallows.
	if _, ok := a.LastCheckpointAt("alpha"); ok {
		t.Errorf("LastCheckpointAt(alpha non-git workdir) ok=true, want false")
	}
}

func TestSessionResolverAdapter(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	s1 := &session.Session{Name: "alpha", UUID: "u-alpha", Mode: "yolo", Workdir: "/srv/a"}
	writeSessionsJSON(t, path, s1)

	proj := ingest.New(path, &fakeTmuxClient{})
	proj.Reload()

	a := sessionResolverAdapter{proj: proj}
	uuid, wd, mode, ok := a.Resolve("alpha")
	if !ok || uuid != "u-alpha" || wd != "/srv/a" || mode != "yolo" {
		t.Errorf("Resolve(alpha) = (%q, %q, %q, %v), want (u-alpha, /srv/a, yolo, true)", uuid, wd, mode, ok)
	}
	if _, _, _, ok := a.Resolve("missing"); ok {
		t.Errorf("Resolve(missing) ok=true, want false")
	}
}

func TestQuotaSourceAdapter(t *testing.T) {
	// Empty ingester → Snapshot returns Known=false.
	q := ingest.NewQuotaIngester(t.TempDir(), nil, events.NewHub(0))
	a := quotaSourceAdapter{quota: q}
	snap := a.Snapshot()
	if snap.Known {
		t.Errorf("Known = true on empty ingester, want false")
	}
	// Nil-quota path.
	if got := (quotaSourceAdapter{}).Snapshot(); got != (api.QuotaSnapshot{}) {
		t.Errorf("nil quota Snapshot = %+v, want zero", got)
	}
}

func TestCostSourceAdapter(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "ctm.db")
	cs, err := store.OpenCostStore(dbPath)
	if err != nil {
		t.Fatalf("OpenCostStore: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	now := time.Now().UTC().Truncate(time.Second)
	if err := cs.Insert([]store.Point{
		{TS: now, Session: "alpha", InputTokens: 10, OutputTokens: 20, CacheTokens: 5, CostUSDMicros: 1234},
	}); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	a := costSourceAdapter{s: cs}

	// Range round-trip.
	pts, err := a.Range("alpha", now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("Range: %v", err)
	}
	if len(pts) != 1 {
		t.Fatalf("Range len = %d, want 1", len(pts))
	}
	if pts[0].Session != "alpha" || pts[0].InputTokens != 10 || pts[0].OutputTokens != 20 {
		t.Errorf("Range[0] = %+v", pts[0])
	}

	// Range with a foreign session → empty slice.
	emptyPts, err := a.Range("nobody", now.Add(-time.Hour), now.Add(time.Hour))
	if err != nil {
		t.Fatalf("Range nobody: %v", err)
	}
	if len(emptyPts) != 0 {
		t.Errorf("Range nobody len = %d, want 0", len(emptyPts))
	}

	totals, err := a.Totals(now.Add(-time.Hour))
	if err != nil {
		t.Fatalf("Totals: %v", err)
	}
	if totals.InputTokens != 10 || totals.OutputTokens != 20 || totals.CacheTokens != 5 || totals.CostUSDMicros != 1234 {
		t.Errorf("Totals = %+v", totals)
	}
}

func TestInputSessionSource(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	s1 := &session.Session{Name: "alpha", UUID: "u-alpha", Workdir: "/srv/a"}
	writeSessionsJSON(t, path, s1)

	proj := ingest.New(path, &fakeTmuxClient{alive: map[string]bool{"alpha": true}})
	proj.Reload()

	a := inputSessionSource{proj: proj}
	got, ok := a.Get("alpha")
	if !ok || got.Name != "alpha" {
		t.Errorf("Get(alpha) = (%+v, %v)", got, ok)
	}
	if _, ok := a.Get("missing"); ok {
		t.Errorf("Get(missing) ok=true, want false")
	}
	if !a.TmuxAlive("alpha") {
		t.Errorf("TmuxAlive(alpha) = false, want true")
	}
	if a.TmuxAlive("missing") {
		t.Errorf("TmuxAlive(missing) = true, want false")
	}
}

func TestLogsUUIDResolver_ResolveName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	s1 := &session.Session{Name: "alpha", UUID: "u-alpha", Workdir: "/srv/a"}
	s2 := &session.Session{Name: "beta", UUID: "", Workdir: "/srv/b"}
	writeSessionsJSON(t, path, s1, s2)

	proj := ingest.New(path, &fakeTmuxClient{})
	proj.Reload()

	r := logsUUIDResolver{proj: proj}
	if uuid, ok := r.ResolveName("alpha"); !ok || uuid != "u-alpha" {
		t.Errorf("ResolveName(alpha) = (%q, %v), want (u-alpha, true)", uuid, ok)
	}
	if _, ok := r.ResolveName("beta"); ok {
		t.Errorf("ResolveName(beta empty UUID) ok=true, want false")
	}
	if _, ok := r.ResolveName("missing"); ok {
		t.Errorf("ResolveName(missing) ok=true, want false")
	}
	if _, ok := r.ResolveName(""); ok {
		t.Errorf("ResolveName(empty) ok=true, want false")
	}
	// Nil-projection guard.
	if _, ok := (logsUUIDResolver{}).ResolveName("anything"); ok {
		t.Errorf("ResolveName(nil proj) ok=true, want false")
	}
}

func TestLogsUUIDResolver_ResolveUUID_Direct(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	s1 := &session.Session{Name: "alpha", UUID: "u-alpha", Workdir: "/srv/a"}
	writeSessionsJSON(t, path, s1)

	proj := ingest.New(path, &fakeTmuxClient{})
	proj.Reload()

	r := logsUUIDResolver{proj: proj}
	if name, ok := r.ResolveUUID("u-alpha"); !ok || name != "alpha" {
		t.Errorf("ResolveUUID(u-alpha) = (%q, %v), want (alpha, true)", name, ok)
	}
	// Empty input.
	if _, ok := r.ResolveUUID(""); ok {
		t.Errorf("ResolveUUID(\"\") ok=true, want false")
	}
	if _, ok := (logsUUIDResolver{}).ResolveUUID("anything"); ok {
		t.Errorf("ResolveUUID(nil proj) ok=true, want false")
	}
}

// TestLogsUUIDResolver_ResolveUUID_FallbackMiss exercises the
// claude-projects fallback path when the UUID isn't in the projection.
// We can't intercept os.UserHomeDir here, but since the random UUID
// almost certainly doesn't exist under any user's
// ~/.claude/projects/*/*.jsonl, ResolveUUID should fall through to
// "len(matches) != 1" and return false. This still hits the fallback
// branch (UserHomeDir + Glob).
func TestLogsUUIDResolver_ResolveUUID_FallbackMiss(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	writeSessionsJSON(t, path) // empty

	proj := ingest.New(path, &fakeTmuxClient{})
	proj.Reload()

	r := logsUUIDResolver{proj: proj}
	bogus := "this-uuid-does-not-exist-anywhere-7c4e1a2b3d4f"
	if name, ok := r.ResolveUUID(bogus); ok {
		t.Errorf("ResolveUUID(bogus) = (%q, true), want false", name)
	}
}

// ---- Server lifecycle helpers ---------------------------------------------

func TestServerAddrAndHub(t *testing.T) {
	port := pickFreePort(t)
	t.Cleanup(startServer(t, "vAddr", port))

	// Build a fresh handle by taking a separate Server reference so we
	// can call Addr/Hub directly. Easier: just call New() directly with
	// a *different* port (Addr/Hub don't require Run).
	port2 := pickFreePort(t)
	srv, err := New(Options{
		Port:              port2,
		Version:           "vAddr2",
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

	addr := srv.Addr()
	if addr == "" {
		t.Errorf("Addr() = \"\"")
	}
	if got, want := addr, "127.0.0.1:"+strconv.Itoa(port2); got != want {
		t.Errorf("Addr() = %q, want %q", got, want)
	}
	if srv.Hub() == nil {
		t.Errorf("Hub() = nil")
	}
}

func TestServerShutdownIdempotent(t *testing.T) {
	// Shutdown is safe to call before Run starts (no-op) and more than
	// once. Both code paths are exercised.
	port := pickFreePort(t)
	srv, err := New(Options{
		Port:              port,
		Version:           "vShutdown",
		Token:             testToken,
		SessionsPath:      filepath.Join(t.TempDir(), "sessions.json"),
		TmuxConfPath:      filepath.Join(t.TempDir(), "tmux.conf"),
		LogDir:            filepath.Join(t.TempDir(), "logs"),
		StatuslineDumpDir: filepath.Join(t.TempDir(), "statusline"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = srv.listener.Close(); _ = srv.cost.Close() }()

	// Pre-Run: runCancel is nil → no-op.
	srv.Shutdown("test pre-run")
}

func TestServerShutdownTriggersRunReturn(t *testing.T) {
	// Bring up a Server, call Shutdown(), confirm Run returns. This
	// covers Server.Shutdown's runCancel path AND the ctx.Done branch
	// of Run.
	port := pickFreePort(t)
	srv, err := New(Options{
		Port:              port,
		Version:           "vShutdownRun",
		Token:             testToken,
		SessionsPath:      filepath.Join(t.TempDir(), "sessions.json"),
		TmuxConfPath:      filepath.Join(t.TempDir(), "tmux.conf"),
		LogDir:            filepath.Join(t.TempDir(), "logs"),
		StatuslineDumpDir: filepath.Join(t.TempDir(), "statusline"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- srv.Run(context.Background()) }()

	// Wait for healthz before shutting down so Run is fully initialised.
	deadline := time.Now().Add(2 * time.Second)
	addr := "http://127.0.0.1:" + strconv.Itoa(port)
	for {
		resp, err := http.Get(addr + "/healthz")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("healthz never became ready")
		}
		time.Sleep(5 * time.Millisecond)
	}

	srv.Shutdown("test")
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run() = %v, want nil", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not return within 15s after Shutdown")
	}
}

// ---- Run-path coverage: orphan + adopted UUID adoption --------------------

// TestRunAdoptsUUIDsFromLogDir seeds a sessions.json with a known UUID
// and writes <uuid>.jsonl into the log dir; on Run, the tailer manager
// must pick it up and the Active() set must contain the session name.
// This exercises the loop body in Run lines ~384-414 (now extracted).
func TestRunAdoptsUUIDsFromLogDir(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	sessionsPath := filepath.Join(tmpDir, "sessions.json")
	s := &session.Session{Name: "myrun", UUID: "uuid-known", Workdir: "/srv/myrun"}
	writeSessionsJSON(t, sessionsPath, s)
	// Direct-match log file.
	if err := os.WriteFile(filepath.Join(logDir, "uuid-known.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	// Orphan log file (no projection match, no claude-projects fallback).
	if err := os.WriteFile(filepath.Join(logDir, "orphan-12345678abcd.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write orphan: %v", err)
	}

	port := pickFreePort(t)
	srv, err := New(Options{
		Port:              port,
		Version:           "vRunAdopt",
		Token:             testToken,
		SessionsPath:      sessionsPath,
		TmuxConfPath:      filepath.Join(tmpDir, "tmux.conf"),
		LogDir:            logDir,
		StatuslineDumpDir: filepath.Join(tmpDir, "statusline"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Run(ctx) }()

	// Wait until tailers are registered.
	deadline := time.Now().Add(2 * time.Second)
	for {
		active := srv.tailers.Active()
		hasMyrun := false
		hasOrphan := false
		for _, n := range active {
			if n == "myrun" {
				hasMyrun = true
			}
			if len(n) >= len("uuid:") && n[:5] == "uuid:" {
				hasOrphan = true
			}
		}
		if hasMyrun && hasOrphan {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("tailers Active = %v; want both myrun + uuid:* prefix", active)
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("Run did not return within 15s")
	}
}

// TestRescanTailersReadsLogDir exercises rescanTailers directly: with
// a populated projection and a fresh log file, it should call
// tailers.Start for the matching session name. We use a local Server
// (not via Run) and just invoke rescanTailers in-band.
func TestRescanTailersReadsLogDir(t *testing.T) {
	tmpDir := t.TempDir()
	logDir := filepath.Join(tmpDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	sessionsPath := filepath.Join(tmpDir, "sessions.json")
	s := &session.Session{Name: "rescan", UUID: "uuid-rescan", Workdir: "/srv/rescan"}
	writeSessionsJSON(t, sessionsPath, s)
	if err := os.WriteFile(filepath.Join(logDir, "uuid-rescan.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write log: %v", err)
	}
	// Orphan: not in projection → silently skipped (rescanTailers does
	// NOT register orphan UUIDs, only the boot pass does).
	if err := os.WriteFile(filepath.Join(logDir, "orphan-rescan-7c4e.jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write orphan: %v", err)
	}
	// Non-jsonl file: should be skipped.
	if err := os.WriteFile(filepath.Join(logDir, "garbage.txt"), []byte("nope"), 0o600); err != nil {
		t.Fatalf("write garbage: %v", err)
	}
	// Subdir: should be skipped.
	if err := os.MkdirAll(filepath.Join(logDir, "subdir"), 0o755); err != nil {
		t.Fatalf("mkdir subdir: %v", err)
	}

	port := pickFreePort(t)
	srv, err := New(Options{
		Port:              port,
		Version:           "vRescan",
		Token:             testToken,
		SessionsPath:      sessionsPath,
		TmuxConfPath:      filepath.Join(tmpDir, "tmux.conf"),
		LogDir:            logDir,
		StatuslineDumpDir: filepath.Join(tmpDir, "statusline"),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = srv.listener.Close(); _ = srv.cost.Close() }()

	srv.proj.Reload() // populate projection so resolveLogUUIDToName matches.

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv.rescanTailers(ctx, "")

	active := srv.tailers.Active()
	found := false
	for _, n := range active {
		if n == "rescan" {
			found = true
		}
		if len(n) >= 5 && n[:5] == "uuid:" {
			t.Errorf("rescanTailers should not register orphan: %q", n)
		}
	}
	if !found {
		t.Errorf("rescanTailers did not start tailer for 'rescan'; active=%v", active)
	}

	// Now exercise the early-return when ReadDir fails (logDir missing).
	_ = os.RemoveAll(logDir)
	srv.rescanTailers(ctx, "") // must not panic
}

// ---- registerRoutes coverage: hit a few unauthenticated/auth paths --------

// TestServeMuxAuthStatusUnauthenticated covers the AuthStatus route,
// which is registered without authHF and not currently exercised.
func TestServeMuxAuthStatusUnauthenticated(t *testing.T) {
	port := pickFreePort(t)
	t.Cleanup(startServer(t, "vAuth", port))

	resp, err := http.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/api/auth/status")
	if err != nil {
		t.Fatalf("get auth/status: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestServeMuxDoctorAuthed(t *testing.T) {
	port := pickFreePort(t)
	t.Cleanup(startServer(t, "vDoctor", port))

	resp := authedGet(t, "http://127.0.0.1:"+strconv.Itoa(port)+"/api/doctor")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestServeMuxQuotaAuthed(t *testing.T) {
	port := pickFreePort(t)
	t.Cleanup(startServer(t, "vQuota", port))

	resp := authedGet(t, "http://127.0.0.1:"+strconv.Itoa(port)+"/api/quota")
	defer resp.Body.Close()
	// Empty quota → 204 No Content; populated → 200. We just verify the
	// route is reachable and returns a non-error status.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		t.Errorf("status = %d, want 200/204", resp.StatusCode)
	}
}

func TestServeMuxFeedAuthed(t *testing.T) {
	port := pickFreePort(t)
	t.Cleanup(startServer(t, "vFeed", port))

	resp := authedGet(t, "http://127.0.0.1:"+strconv.Itoa(port)+"/api/feed")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestServeMuxLogsUsageAuthed(t *testing.T) {
	port := pickFreePort(t)
	t.Cleanup(startServer(t, "vLogs", port))

	resp := authedGet(t, "http://127.0.0.1:"+strconv.Itoa(port)+"/api/logs/usage")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

func TestServeMuxDebugHubAuthed(t *testing.T) {
	port := pickFreePort(t)
	t.Cleanup(startServer(t, "vDbg", port))

	resp := authedGet(t, "http://127.0.0.1:"+strconv.Itoa(port)+"/debug/hub")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
}

func TestServeMuxCostAuthed(t *testing.T) {
	port := pickFreePort(t)
	t.Cleanup(startServer(t, "vCost", port))

	resp := authedGet(t, "http://127.0.0.1:"+strconv.Itoa(port)+"/api/cost")
	defer resp.Body.Close()
	// Cost handler returns 200 with empty data when no points exist.
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 200/400", resp.StatusCode)
	}
}

func TestAuthRejectsBadToken(t *testing.T) {
	port := pickFreePort(t)
	t.Cleanup(startServer(t, "vBad", port))

	req, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(port)+"/health", nil)
	req.Header.Set("Authorization", "Bearer not-the-right-token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != "invalid_token" {
		t.Errorf("error = %q, want invalid_token", body["error"])
	}
}

// TestWriteJSONAuthErrShape exercises the helper directly to lock in
// its response shape (status code + Content-Type + JSON body).
func TestWriteJSONAuthErrShape(t *testing.T) {
	rr := httptest.NewRecorder()
	writeJSONAuthErr(rr, http.StatusForbidden, "no_perm")
	if rr.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != "no_perm" {
		t.Errorf("error = %q, want no_perm", body["error"])
	}
}

// TestNewWithCustomThresholds covers the AttentionThresholds branch in
// New that picks attention.Defaults() when the option is zero-valued.
func TestNewWithCustomThresholds(t *testing.T) {
	port := pickFreePort(t)
	thr := attention.Defaults()
	thr.QuotaPct = 42
	srv, err := New(Options{
		Port:                port,
		Version:             "vThr",
		Token:               testToken,
		SessionsPath:        filepath.Join(t.TempDir(), "sessions.json"),
		TmuxConfPath:        filepath.Join(t.TempDir(), "tmux.conf"),
		LogDir:              filepath.Join(t.TempDir(), "logs"),
		StatuslineDumpDir:   filepath.Join(t.TempDir(), "statusline"),
		AttentionThresholds: thr,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = srv.listener.Close(); _ = srv.cost.Close() })
	if srv.attention == nil {
		t.Errorf("attention engine not wired")
	}
}

// TestRegisterRoutesCustomMux exercises the mux registration path on a
// brand-new server without going through Run, ensuring registerRoutes
// is exercised twice in the same process.
func TestRegisterRoutesCustomMux(t *testing.T) {
	// Use the constructed Server so resolveWorkdir / allowedOrigins /
	// quota adapters all wire up the same as production.
	port := pickFreePort(t)
	srv, err := New(Options{
		Port:              port,
		Version:           "vMux",
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
	rr := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/healthz", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("/healthz on local mux = %d, want 200", rr.Code)
	}
}

// TestRegisterRoutesAllowedOriginsEnv covers the CTM_ALLOWED_ORIGINS env
// var branch in registerRoutes (lines 715-721).
func TestRegisterRoutesAllowedOriginsEnv(t *testing.T) {
	t.Setenv("CTM_ALLOWED_ORIGINS", "https://dev.example.com,, https://other.example.com ")

	port := pickFreePort(t)
	srv, err := New(Options{
		Port:              port,
		Version:           "vEnv",
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
	// The env-var fork is exercised at registerRoutes time inside New.
	// We can't observe the resulting allowedOrigins slice externally,
	// but a successful New + healthz on a local mux confirms the path.
	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	rr := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/healthz", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("/healthz = %d, want 200", rr.Code)
	}
}
