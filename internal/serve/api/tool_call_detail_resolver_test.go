package api

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/serve/ingest"
)

// writeSessionsFile creates a minimal sessions.json file at path
// containing the given (name, uuid) pairs so a Projection can resolve
// them via Get(). Format mirrors internal/session/state.go diskShape.
func writeSessionsFile(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	type sess struct {
		Name string `json:"name"`
		UUID string `json:"uuid"`
		Mode string `json:"mode"`
	}
	body := map[string]any{
		"schema_version": 1,
		"sessions":       map[string]sess{},
	}
	sessions := body["sessions"].(map[string]sess)
	for name, uuid := range entries {
		sessions[name] = sess{Name: name, UUID: uuid, Mode: "ask"}
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write sessions.json: %v", err)
	}
}

// TestNewJSONLLogReader_HappyPathResolvesViaProjection covers
// NewJSONLLogReader and its embedded projUUIDAdapter.ResolveName: the
// adapter must return the real UUID for known sessions, and surface
// ErrDetailNotFound for unknown ones.
func TestNewJSONLLogReader_ResolvesViaProjection(t *testing.T) {
	dir := t.TempDir()
	logDir := filepath.Join(dir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	sessionsPath := filepath.Join(dir, "sessions.json")
	writeSessionsFile(t, sessionsPath, map[string]string{
		"alpha": "11112222-3333-4444-5555-666677778888",
	})

	proj := ingest.New(sessionsPath, nil)
	proj.Reload()

	r := NewJSONLLogReader(logDir, proj)
	if r == nil {
		t.Fatalf("NewJSONLLogReader returned nil")
	}
	if r.LogDir != logDir {
		t.Errorf("LogDir = %q, want %q", r.LogDir, logDir)
	}
	if r.Resolver == nil {
		t.Fatalf("Resolver is nil")
	}

	// Known session: ResolveName returns the configured UUID.
	uuid, ok := r.Resolver.ResolveName("alpha")
	if !ok || uuid != "11112222-3333-4444-5555-666677778888" {
		t.Errorf("ResolveName(alpha) = (%q, %v), want (uuid, true)", uuid, ok)
	}

	// Unknown session: ResolveName reports !ok.
	if _, ok := r.Resolver.ResolveName("ghost"); ok {
		t.Errorf("ResolveName(ghost) = ok, want !ok")
	}

	// ReadDetail for an unknown session must return ErrDetailNotFound
	// without ever touching the filesystem.
	if _, err := r.ReadDetail("ghost", "anything-0"); !errors.Is(err, ErrDetailNotFound) {
		t.Errorf("ReadDetail(ghost) err = %v, want ErrDetailNotFound", err)
	}

	// ReadDetail for a known session whose JSONL file does not exist
	// should also return ErrDetailNotFound (os.ErrNotExist mapped).
	if _, err := r.ReadDetail("alpha", "anything-0"); !errors.Is(err, ErrDetailNotFound) {
		t.Errorf("ReadDetail(alpha, missing file) err = %v, want ErrDetailNotFound", err)
	}
}

// TestNewJSONLLogReader_NilReceiverSafe — paranoia for the early
// "r == nil || r.Resolver == nil" guard in ReadDetail.
func TestJSONLLogReader_NilReceiverReturnsNotFound(t *testing.T) {
	var r *JSONLLogReader
	if _, err := r.ReadDetail("anything", "id-0"); !errors.Is(err, ErrDetailNotFound) {
		t.Errorf("nil receiver err = %v, want ErrDetailNotFound", err)
	}
}

// TestProjUUIDAdapter_EmptyUUIDIsNotResolvable — the adapter's "uuid != ''"
// guard ensures sessions with no Claude UUID surface as not-resolvable
// rather than returning an empty string that would later look up the
// wrong file.
func TestProjUUIDAdapter_EmptyUUIDNotResolvable(t *testing.T) {
	dir := t.TempDir()
	sessionsPath := filepath.Join(dir, "sessions.json")
	// alpha has no UUID.
	writeSessionsFile(t, sessionsPath, map[string]string{"alpha": ""})

	proj := ingest.New(sessionsPath, nil)
	proj.Reload()

	r := NewJSONLLogReader(dir, proj)
	uuid, ok := r.Resolver.ResolveName("alpha")
	if ok || uuid != "" {
		t.Errorf("ResolveName(alpha, blank uuid) = (%q, %v), want (\"\", false)", uuid, ok)
	}
}
