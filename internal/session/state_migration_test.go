package session_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/migrate"
	"github.com/RandomCodeSpace/ctm/internal/session"
)

// TestMigration_V1ToV2_BackfillsAgentClaude verifies that the v1→v2
// migration step stamps agent="claude" on any session row missing the
// field, and leaves rows that already have an agent untouched.
func TestMigration_V1ToV2_BackfillsAgentClaude(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	v1 := `{
  "schema_version": 1,
  "sessions": {
    "old1": {"name":"old1","uuid":"u-1","mode":"yolo","workdir":"/tmp"},
    "old2": {"name":"old2","uuid":"u-2","mode":"safe","workdir":"/tmp","agent":"claude"}
  }
}`
	if err := os.WriteFile(path, []byte(v1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Run(path, session.MigrationPlan()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	raw, _ := os.ReadFile(path)
	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var sv int
	_ = json.Unmarshal(got["schema_version"], &sv)
	if sv != 2 {
		t.Fatalf("schema_version = %d, want 2", sv)
	}
	var sessions map[string]map[string]any
	if err := json.Unmarshal(got["sessions"], &sessions); err != nil {
		t.Fatalf("sessions unmarshal: %v", err)
	}
	for name, row := range sessions {
		if row["agent"] != "claude" {
			t.Fatalf("session[%s].agent = %v, want \"claude\"", name, row["agent"])
		}
	}
}

// TestMigration_V1ToV2_Idempotent verifies the step is safe to re-run.
// The migrate runner short-circuits when already at CurrentVersion, but
// the underlying step must also be a no-op if called against an
// already-backfilled object.
func TestMigration_V1ToV2_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	v1 := `{
  "schema_version": 1,
  "sessions": {
    "s": {"name":"s","uuid":"u","mode":"yolo","workdir":"/tmp"}
  }
}`
	if err := os.WriteFile(path, []byte(v1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := migrate.Run(path, session.MigrationPlan()); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	first, _ := os.ReadFile(path)
	if _, err := migrate.Run(path, session.MigrationPlan()); err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Fatalf("second migrate altered the file\nfirst:  %s\nsecond: %s", first, second)
	}
}

// TestSession_AgentFieldRoundTrip verifies that the Agent and
// AgentSessionID fields survive Save / Get cycle.
func TestSession_AgentFieldRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(filepath.Join(dir, "sessions.json"))
	in := &session.Session{
		Name:           "foo",
		UUID:           "u-foo",
		Mode:           "yolo",
		Workdir:        "/tmp",
		Agent:          "claude",
		AgentSessionID: "u-foo",
	}
	if err := store.Save(in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := store.Get("foo")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if out.Agent != "claude" {
		t.Fatalf("Agent = %q, want claude", out.Agent)
	}
	if out.AgentSessionID != "u-foo" {
		t.Fatalf("AgentSessionID = %q, want u-foo", out.AgentSessionID)
	}
}

// TestSession_EmptyAgentDefaultsClaudeOnSave verifies the Task 03
// normalization step: Save sets s.Agent = "claude" when empty.
// Strict registry validation is deferred to Task 06.
func TestSession_EmptyAgentDefaultsClaudeOnSave(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(filepath.Join(dir, "sessions.json"))
	in := &session.Session{
		Name:    "bar",
		UUID:    "u-bar",
		Mode:    "safe",
		Workdir: "/tmp",
		// Agent intentionally empty
	}
	if err := store.Save(in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, _ := store.Get("bar")
	if out.Agent != "claude" {
		t.Fatalf("empty Agent should default to \"claude\" on Save, got %q", out.Agent)
	}
}
