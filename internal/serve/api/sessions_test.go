package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/api"
	"github.com/RandomCodeSpace/ctm/internal/serve/ingest"
	"github.com/RandomCodeSpace/ctm/internal/session"
)

type fakeTmux struct {
	mu    sync.Mutex
	alive map[string]bool
}

func (f *fakeTmux) HasSession(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.alive[name]
}

func writeSessions(t *testing.T, path string, sessions ...*session.Session) {
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
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// boot constructs a projection bound to a temp sessions.json prefilled
// with sessions, runs it until ready, and returns it plus the fake tmux
// (so tests can flip alive state).
func boot(t *testing.T, alive map[string]bool, sessions ...*session.Session) (*ingest.Projection, *fakeTmux) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	writeSessions(t, path, sessions...)

	tx := &fakeTmux{alive: alive}
	p := ingest.New(path, tx)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go func() { _ = p.Run(ctx) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(p.All()) == len(sessions) {
			return p, tx
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("projection never reached %d sessions; All=%v", len(sessions), p.All())
	return nil, nil
}

func TestList_OmitsUnpopulatedFields(t *testing.T) {
	s := session.New("alpha", "/work/alpha", "safe")
	p, _ := boot(t, map[string]bool{"alpha": true}, s)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	api.List(p, nil)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	var out []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 session in list, got %d (%v)", len(out), out)
	}
	got := out[0]
	if got["name"] != "alpha" {
		t.Errorf("name = %v", got["name"])
	}
	if got["tmux_alive"] != true {
		t.Errorf("tmux_alive = %v, want true", got["tmux_alive"])
	}
	if got["is_active"] != true {
		t.Errorf("is_active = %v, want true", got["is_active"])
	}
	for _, key := range []string{"last_tool_call_at", "context_pct", "attention"} {
		if _, present := got[key]; present {
			t.Errorf("%q must be omitted when enricher reports no value; got %v", key, got[key])
		}
	}
	// last_attached_at is omitempty when zero — newly minted session has zero LastAttachedAt.
	if _, present := got["last_attached_at"]; present {
		t.Errorf("last_attached_at must be omitted for never-attached session; got %v", got["last_attached_at"])
	}
}

func TestGet_UnknownReturns404(t *testing.T) {
	s := session.New("alpha", "/work/alpha", "safe")
	p, _ := boot(t, map[string]bool{"alpha": true}, s)

	mux := http.NewServeMux()
	mux.Handle("GET /api/sessions/{name}", api.Get(p, nil))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/ghost", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] == nil {
		t.Errorf("expected error field in 404 body, got %v", body)
	}
}

func TestGet_KnownReflectsTmuxAlive(t *testing.T) {
	s := session.New("alpha", "/work/alpha", "yolo")
	p, _ := boot(t, map[string]bool{"alpha": false}, s) // tmux says dead

	mux := http.NewServeMux()
	mux.Handle("GET /api/sessions/{name}", api.Get(p, nil))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/alpha", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["name"] != "alpha" {
		t.Errorf("name = %v, want alpha", body["name"])
	}
	if body["tmux_alive"] != false {
		t.Errorf("tmux_alive = %v, want false", body["tmux_alive"])
	}
	if body["is_active"] != false {
		t.Errorf("is_active = %v, want false", body["is_active"])
	}
	if body["mode"] != "yolo" {
		t.Errorf("mode = %v, want yolo", body["mode"])
	}
	if body["uuid"] != s.UUID {
		t.Errorf("uuid = %v, want %v", body["uuid"], s.UUID)
	}
	for _, key := range []string{"last_tool_call_at", "context_pct", "attention"} {
		if _, present := body[key]; present {
			t.Errorf("%q must be omitted by NoopEnricher; got %v", key, body[key])
		}
	}
}

// stubEnricher returns canned values to verify the wiring path when a
// real enricher is plugged in by a later step.
type stubEnricher struct{}

func (stubEnricher) LastToolCallAt(string) (time.Time, bool) {
	return time.Date(2026, 4, 20, 15, 30, 42, 0, time.UTC), true
}
func (stubEnricher) ContextPct(string) (int, bool) { return 49, true }
func (stubEnricher) Attention(string) (api.Attention, bool) {
	return api.Attention{
		State:   "error_burst",
		Since:   time.Date(2026, 4, 20, 15, 29, 10, 0, time.UTC),
		Details: "6 errors in last 20 calls",
	}, true
}
func (stubEnricher) Tokens(string) (api.TokenUsage, bool) {
	return api.TokenUsage{InputTokens: 17, OutputTokens: 42, CacheTokens: 8192}, true
}

func TestGet_EnricherFieldsPropagate(t *testing.T) {
	s := session.New("alpha", "/work/alpha", "yolo")
	p, _ := boot(t, map[string]bool{"alpha": true}, s)

	mux := http.NewServeMux()
	mux.Handle("GET /api/sessions/{name}", api.Get(p, stubEnricher{}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/alpha", nil)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["last_tool_call_at"] == nil {
		t.Errorf("last_tool_call_at missing; body=%v", body)
	}
	if pct, ok := body["context_pct"].(float64); !ok || pct != 49 {
		t.Errorf("context_pct = %v, want 49", body["context_pct"])
	}
	att, ok := body["attention"].(map[string]any)
	if !ok {
		t.Fatalf("attention missing or wrong type: %v", body["attention"])
	}
	if att["state"] != "error_burst" {
		t.Errorf("attention.state = %v", att["state"])
	}
	if att["details"] != "6 errors in last 20 calls" {
		t.Errorf("attention.details = %v", att["details"])
	}
	tokens, ok := body["tokens"].(map[string]any)
	if !ok {
		t.Fatalf("tokens missing or wrong type: %v", body["tokens"])
	}
	if in, _ := tokens["input_tokens"].(float64); in != 17 {
		t.Errorf("tokens.input_tokens = %v, want 17", tokens["input_tokens"])
	}
	if out, _ := tokens["output_tokens"].(float64); out != 42 {
		t.Errorf("tokens.output_tokens = %v, want 42", tokens["output_tokens"])
	}
	if c, _ := tokens["cache_tokens"].(float64); c != 8192 {
		t.Errorf("tokens.cache_tokens = %v, want 8192", tokens["cache_tokens"])
	}
}
