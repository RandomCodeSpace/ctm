package session_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/migrate"
	"github.com/RandomCodeSpace/ctm/internal/session"
)

// TestMigration_V1ToV3_RewritesAllToCodex verifies the full v1 → v3
// migration path: legacy rows missing `agent` get stamped (v1→v2) and
// every row ends at "codex" (v2→v3 rewrite of legacy "claude" values).
func TestMigration_V1ToV3_RewritesAllToCodex(t *testing.T) {
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
	if sv != 3 {
		t.Fatalf("schema_version = %d, want 3", sv)
	}
	var sessions map[string]map[string]any
	if err := json.Unmarshal(got["sessions"], &sessions); err != nil {
		t.Fatalf("sessions unmarshal: %v", err)
	}
	for name, row := range sessions {
		if row["agent"] != "codex" {
			t.Fatalf("session[%s].agent = %v, want \"codex\"", name, row["agent"])
		}
	}
}

// TestMigration_V2ToV3_RewritesClaudeRowsOnly verifies isolated v2→v3
// behavior: only agent="claude" rows are rewritten; non-claude agents
// (e.g. a future "opencode") are left alone.
func TestMigration_V2ToV3_RewritesClaudeRowsOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	v2 := `{
  "schema_version": 2,
  "sessions": {
    "legacy":  {"name":"legacy","uuid":"u-1","mode":"yolo","workdir":"/tmp","agent":"claude"},
    "other":   {"name":"other","uuid":"u-2","mode":"safe","workdir":"/tmp","agent":"opencode"}
  }
}`
	if err := os.WriteFile(path, []byte(v2), 0600); err != nil {
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
	var sessions map[string]map[string]any
	if err := json.Unmarshal(got["sessions"], &sessions); err != nil {
		t.Fatalf("sessions unmarshal: %v", err)
	}
	if sessions["legacy"]["agent"] != "codex" {
		t.Fatalf("legacy.agent = %v, want \"codex\"", sessions["legacy"]["agent"])
	}
	if sessions["other"]["agent"] != "opencode" {
		t.Fatalf("other.agent = %v, want \"opencode\" (untouched)", sessions["other"]["agent"])
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
// AgentSessionID fields survive a Save / Get cycle for non-default
// agents. (The default agent path is covered by
// TestSession_EmptyAgentDefaultsCodexOnSave.)
func TestSession_AgentFieldRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(filepath.Join(dir, "sessions.json"))
	in := &session.Session{
		Name:           "foo",
		UUID:           "u-foo",
		Mode:           "yolo",
		Workdir:        "/tmp",
		Agent:          "codex",
		AgentSessionID: "thread-foo",
	}
	if err := store.Save(in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, err := store.Get("foo")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if out.Agent != "codex" {
		t.Fatalf("Agent = %q, want codex", out.Agent)
	}
	if out.AgentSessionID != "thread-foo" {
		t.Fatalf("AgentSessionID = %q, want thread-foo", out.AgentSessionID)
	}
}

// TestSession_EmptyAgentDefaultsCodexOnSave verifies the read-side
// guard: Save sets s.Agent = "codex" when empty.
func TestSession_EmptyAgentDefaultsCodexOnSave(t *testing.T) {
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
	if out.Agent != "codex" {
		t.Fatalf("empty Agent should default to \"codex\" on Save, got %q", out.Agent)
	}
}

// TestSession_LegacyClaudeRewrittenOnSave verifies that an in-memory
// Session with Agent="claude" (e.g. read from a v2 file by code that
// skipped the migration runner) is rewritten to "codex" by Save.
func TestSession_LegacyClaudeRewrittenOnSave(t *testing.T) {
	dir := t.TempDir()
	store := session.NewStore(filepath.Join(dir, "sessions.json"))
	in := &session.Session{
		Name:    "legacy",
		UUID:    "u-legacy",
		Mode:    "safe",
		Workdir: "/tmp",
		Agent:   "claude",
	}
	if err := store.Save(in); err != nil {
		t.Fatalf("save: %v", err)
	}
	out, _ := store.Get("legacy")
	if out.Agent != "codex" {
		t.Fatalf("legacy claude row should be rewritten to codex on Save, got %q", out.Agent)
	}
}
