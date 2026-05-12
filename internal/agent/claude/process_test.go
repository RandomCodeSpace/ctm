package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsClaudePID_InvalidPID(t *testing.T) {
	alive, err := IsClaudeAlive("")
	if err == nil {
		t.Error("expected error for empty PID, got nil")
	}
	if alive {
		t.Error("expected not alive for empty PID")
	}
}

func TestIsClaudePID_NonExistent(t *testing.T) {
	alive, err := IsClaudeAlive("9999999")
	if err != nil {
		t.Errorf("expected no error for non-existent PID, got: %v", err)
	}
	if alive {
		t.Error("expected not alive for non-existent PID")
	}
}

func TestFindClaudeChild_NoPID(t *testing.T) {
	pid := FindClaudeChild("")
	if pid != "" {
		t.Errorf("expected empty string for empty panePID, got: %s", pid)
	}
}

func TestSessionExistsWalkFindsUUIDInJSON(t *testing.T) {
	dir := t.TempDir()
	uuid := "f6489cb4-010f-4c96-940b-188014f746f0"
	path := filepath.Join(dir, "session.json")
	content := `{"sessionId":"` + uuid + `","messages":[]}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	if !sessionExistsWalk(dir, uuid) {
		t.Errorf("expected to find uuid %s in %s", uuid, dir)
	}
}

func TestSessionExistsWalkMissing(t *testing.T) {
	dir := t.TempDir()
	if sessionExistsWalk(dir, "not-in-any-file") {
		t.Error("expected false for missing uuid")
	}
}

func TestSessionExistsWalkIgnoresNonJSON(t *testing.T) {
	dir := t.TempDir()
	uuid := "abc123"
	if err := os.WriteFile(filepath.Join(dir, "data.txt"), []byte(uuid), 0644); err != nil {
		t.Fatal(err)
	}
	if sessionExistsWalk(dir, uuid) {
		t.Error("expected false — UUID only in .txt file")
	}
}

func TestSessionExistsWalkRecursive(t *testing.T) {
	dir := t.TempDir()
	uuid := "nested-uuid-here"
	nested := filepath.Join(dir, "projects", "myproject", "sessions")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(nested, "deep.json")
	if err := os.WriteFile(path, []byte(`{"id":"`+uuid+`"}`), 0644); err != nil {
		t.Fatal(err)
	}
	if !sessionExistsWalk(dir, uuid) {
		t.Errorf("expected to find uuid nested inside %s", dir)
	}
}

func TestSessionExistsWalkNonexistentDir(t *testing.T) {
	if sessionExistsWalk("/nonexistent/path/xyz", "anything") {
		t.Error("expected false for nonexistent dir")
	}
}

func TestIsNumeric(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"123", true},
		{"0", true},
		{"9999999", true},
		{"", false},
		{"12a", false},
		{"abc", false},
		{"-1", false},
		{"1.5", false},
		{" 1", false},
	}
	for _, tt := range tests {
		if got := isNumeric(tt.in); got != tt.want {
			t.Errorf("isNumeric(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestReadProcStatusParsesSelf(t *testing.T) {
	path := "/proc/self/status"
	if _, err := os.Stat(path); err != nil {
		t.Skip("/proc not available, skipping")
	}
	ppid, name, ok := readProcStatus(path)
	if !ok {
		t.Fatal("expected ok=true for /proc/self/status")
	}
	if name == "" {
		t.Error("expected non-empty Name")
	}
	if !isNumeric(ppid) {
		t.Errorf("expected numeric PPid, got %q", ppid)
	}
}

func TestReadProcStatusMissing(t *testing.T) {
	_, _, ok := readProcStatus("/nonexistent/proc/entry")
	if ok {
		t.Error("expected ok=false for missing path")
	}
}
