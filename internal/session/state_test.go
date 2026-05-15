package session_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/session"
)

func TestNewSession(t *testing.T) {
	s := session.New("mysession", "/tmp/work", "safe")
	if s.Name != "mysession" {
		t.Errorf("expected Name=mysession, got %s", s.Name)
	}
	if s.Workdir != "/tmp/work" {
		t.Errorf("expected Workdir=/tmp/work, got %s", s.Workdir)
	}
	if s.Mode != "safe" {
		t.Errorf("expected Mode=safe, got %s", s.Mode)
	}
	if s.UUID == "" {
		t.Error("expected non-empty UUID")
	}
	if s.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
}

func TestValidateName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"simple alpha", "mysession", false},
		{"with dash", "my-session", false},
		{"with dot", "my.session", true},
		{"with underscore", "my_session", false},
		{"alphanumeric", "session123", false},
		{"single char", "a", false},
		{"max length 100", strings.Repeat("a", 100), false},
		{"empty string", "", true},
		{"with space", "my session", true},
		{"with slash", "my/session", true},
		{"with colon", "my:session", true},
		{"too long 101", strings.Repeat("a", 101), true},
		{"starts with dot", ".hidden", true},
		{"starts with dash", "-bad", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := session.ValidateName(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateName(%q) error=%v, wantErr=%v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func newStore(t *testing.T) *session.Store {
	t.Helper()
	dir := t.TempDir()
	return session.NewStore(filepath.Join(dir, "sessions.json"))
}

func TestStoreLoadRoundTrip(t *testing.T) {
	st := newStore(t)
	s := session.New("alpha", "/work/alpha", "safe")
	if err := st.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := st.Get("alpha")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.UUID != s.UUID {
		t.Errorf("UUID mismatch: got %s want %s", got.UUID, s.UUID)
	}
	if got.Mode != "safe" {
		t.Errorf("Mode mismatch: got %s", got.Mode)
	}
}

func TestStoreList(t *testing.T) {
	st := newStore(t)
	st.Save(session.New("alpha", "/work", "safe"))
	st.Save(session.New("beta", "/work", "yolo"))
	list, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(list))
	}
}

func TestStoreDelete(t *testing.T) {
	st := newStore(t)
	st.Save(session.New("alpha", "/work", "safe"))
	if err := st.Delete("alpha"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err := st.Get("alpha")
	if err == nil {
		t.Error("expected error getting deleted session")
	}
}

func TestStoreDeleteAll(t *testing.T) {
	st := newStore(t)
	st.Save(session.New("alpha", "/work", "safe"))
	st.Save(session.New("beta", "/work", "yolo"))
	if err := st.DeleteAll(); err != nil {
		t.Fatalf("DeleteAll: %v", err)
	}
	list, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(list))
	}
}

func TestStoreRename(t *testing.T) {
	st := newStore(t)
	st.Save(session.New("old", "/work", "safe"))
	if err := st.Rename("old", "new"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if _, err := st.Get("old"); err == nil {
		t.Error("expected old name to be gone")
	}
	got, err := st.Get("new")
	if err != nil {
		t.Fatalf("Get new: %v", err)
	}
	if got.Name != "new" {
		t.Errorf("expected Name=new, got %s", got.Name)
	}
}

func TestStoreUpdateMode(t *testing.T) {
	st := newStore(t)
	st.Save(session.New("alpha", "/work", "safe"))
	if err := st.UpdateMode("alpha", "yolo"); err != nil {
		t.Fatalf("UpdateMode: %v", err)
	}
	got, _ := st.Get("alpha")
	if got.Mode != "yolo" {
		t.Errorf("expected Mode=yolo, got %s", got.Mode)
	}
}

func TestAtomicWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	st := session.NewStore(path)

	if err := st.Save(session.New("alpha", "/work", "safe")); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// No .tmp file should remain after a successful write.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("expected .tmp file to be gone after successful write")
	}
	// The real file must exist.
	if _, err := os.Stat(path); err != nil {
		t.Errorf("sessions.json should exist: %v", err)
	}
}

func TestCorruptRecovery(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	if err := os.WriteFile(path, []byte("not valid json {{{"), 0644); err != nil {
		t.Fatal(err)
	}
	st := session.NewStore(path)

	// Load should succeed, returning empty state.
	list, err := st.List()
	if err != nil {
		t.Fatalf("expected corrupt file to be recovered, got error: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected empty list after corrupt recovery, got %d", len(list))
	}

	// A .corrupt.* backup file must exist.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if strings.Contains(e.Name(), ".corrupt.") {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected a .corrupt.<timestamp> backup file to be created")
	}
}

func TestBackup(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	st := session.NewStore(path)

	s := session.New("alpha", "/work/alpha", "safe")
	if err := st.Save(s); err != nil {
		t.Fatalf("Save: %v", err)
	}

	backupPath, err := st.Backup()
	if err != nil {
		t.Fatalf("Backup: %v", err)
	}
	if backupPath == "" {
		t.Fatal("expected non-empty backup path")
	}

	// Backup file must exist.
	data, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("reading backup: %v", err)
	}
	if !strings.Contains(string(data), "alpha") {
		t.Errorf("backup content doesn't contain session name 'alpha': %s", data)
	}
	if !strings.Contains(backupPath, ".bak.") {
		t.Errorf("backup path should contain .bak.: %s", backupPath)
	}
}

func TestStoreGetNonExistent(t *testing.T) {
	st := newStore(t)
	_, err := st.Get("ghost")
	if err == nil {
		t.Error("expected error getting non-existent session")
	}
}

func TestSanitizeName(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"my project", "my-project"},
		{"hello/world", "hello-world"},
		{"--leading-dashes", "leading-dashes"},
		{"__leading_underscores", "leading_underscores"},
		{"valid-name_123", "valid-name_123"},
		{"hello.world", "hello-world"},
		{"---", "session"},
		{"", "session"},
		{strings.Repeat("a", 150), strings.Repeat("a", 100)},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := session.SanitizeName(tt.input)
			if got != tt.want {
				t.Errorf("SanitizeName(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestSaveStampsSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	st := session.NewStore(path)

	if err := st.Save(session.New("alpha", "/work", "safe")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := strconv.Itoa(session.SchemaVersion)
	if v := string(raw["schema_version"]); v != want {
		t.Errorf("sessions.json schema_version = %s, want %s", v, want)
	}
}

func TestMigrationPlan_MatchesSchemaVersion(t *testing.T) {
	p := session.MigrationPlan()
	if p.CurrentVersion != session.SchemaVersion {
		t.Errorf("MigrationPlan.CurrentVersion = %d, want %d", p.CurrentVersion, session.SchemaVersion)
	}
	if len(p.Steps) != session.SchemaVersion {
		t.Errorf("MigrationPlan has %d steps, want %d", len(p.Steps), session.SchemaVersion)
	}
}

// TestNormalizeAgent_DefaultsCodex covers the read-side helper. Empty
// values default to "codex" (the post-claude-removal default); legacy
// "claude" values are also remapped to "codex" so a stale Session
// never surfaces as an agent.For miss at the call site. Other agent
// names pass through verbatim.
func TestNormalizeAgent_DefaultsCodex(t *testing.T) {
	s := &session.Session{}
	if got := s.NormalizeAgent(); got != "codex" {
		t.Fatalf("NormalizeAgent on zero-value = %q, want codex", got)
	}
	s.Agent = "claude"
	if got := s.NormalizeAgent(); got != "codex" {
		t.Fatalf("NormalizeAgent(claude) = %q, want codex (legacy remap)", got)
	}
	s.Agent = "codex"
	if got := s.NormalizeAgent(); got != "codex" {
		t.Fatalf("NormalizeAgent(codex) = %q, want codex", got)
	}
	s.Agent = "opencode"
	if got := s.NormalizeAgent(); got != "opencode" {
		t.Fatalf("NormalizeAgent(opencode) = %q, want opencode (forward-compat)", got)
	}
}

func TestSavePermIs0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	st := session.NewStore(path)
	if err := st.Save(session.New("alpha", "/work", "safe")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("sessions.json mode = %v, want 0600", mode)
	}
}
