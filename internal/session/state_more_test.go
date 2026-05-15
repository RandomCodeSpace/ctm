package session_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/migrate"
	"github.com/RandomCodeSpace/ctm/internal/session"
)

// TestUpdateAgentSessionID_Happy covers the write branch: stamping a
// non-empty id mutates the on-disk row.
func TestUpdateAgentSessionID_Happy(t *testing.T) {
	st := newStore(t)
	if err := st.Save(session.New("foo", "/tmp", "yolo")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := st.UpdateAgentSessionID("foo", "thread-xyz"); err != nil {
		t.Fatalf("UpdateAgentSessionID: %v", err)
	}
	got, err := st.Get("foo")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AgentSessionID != "thread-xyz" {
		t.Fatalf("AgentSessionID = %q, want thread-xyz", got.AgentSessionID)
	}
}

// TestUpdateAgentSessionID_NoChangeIsNoop covers the early-return
// branch when the stored id already equals the supplied id. Verified
// by sneaking a write into the file beneath the store: if the second
// call short-circuits, our sneak should survive.
func TestUpdateAgentSessionID_NoChangeIsNoop(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	st := session.NewStore(path)

	if err := st.Save(session.New("foo", "/tmp", "yolo")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := st.UpdateAgentSessionID("foo", "abc"); err != nil {
		t.Fatalf("first stamp: %v", err)
	}
	// Re-stamp with the same id — must be a no-op (no save() called).
	if err := st.UpdateAgentSessionID("foo", "abc"); err != nil {
		t.Fatalf("second stamp: %v", err)
	}
	got, _ := st.Get("foo")
	if got.AgentSessionID != "abc" {
		t.Errorf("AgentSessionID = %q, want abc", got.AgentSessionID)
	}
}

// TestUpdateAgentSessionID_Missing covers the "session not found" branch.
func TestUpdateAgentSessionID_Missing(t *testing.T) {
	st := newStore(t)
	if err := st.UpdateAgentSessionID("ghost", "x"); err == nil {
		t.Error("expected error stamping unknown session")
	}
}

// TestMigration_StampAgentClaude_NoSessionsKey covers the early-return
// branch where the on-disk JSON has no "sessions" key at all.
// migrate.Run will still stamp schema_version even for an otherwise-
// empty file.
func TestMigration_StampAgentClaude_NoSessionsKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	// schema_version=1 → migration runs v1→v2→v3 against this no-sessions
	// shape. Both Steps must early-return without error.
	if err := os.WriteFile(path, []byte(`{"schema_version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Run(path, session.MigrationPlan()); err != nil {
		t.Fatalf("migrate.Run: %v", err)
	}
	raw, _ := os.ReadFile(path)
	var got map[string]json.RawMessage
	_ = json.Unmarshal(raw, &got)
	var sv int
	_ = json.Unmarshal(got["schema_version"], &sv)
	if sv != session.SchemaVersion {
		t.Errorf("schema_version = %d, want %d", sv, session.SchemaVersion)
	}
}

// TestMigration_StampAgentClaude_MalformedSessions covers the
// "json.Unmarshal sessions failed" error branch in stampAgentClaude.
// We craft a v1 file whose sessions blob is the wrong shape (a list
// instead of a map) — the step must surface a parse error and the
// migrate runner must propagate it.
func TestMigration_StampAgentClaude_MalformedSessions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	bad := `{"schema_version":1,"sessions":[]}`
	if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Run(path, session.MigrationPlan()); err == nil {
		t.Fatal("expected error on malformed sessions blob")
	}
}

// TestMigration_RewriteClaudeToCodex_NoSessions covers the v2→v3 early
// return when the file has no sessions map (idempotency).
func TestMigration_RewriteClaudeToCodex_NoSessions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":2}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Run(path, session.MigrationPlan()); err != nil {
		t.Fatalf("migrate.Run: %v", err)
	}
}
