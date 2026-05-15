package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CheckWorkdir is the pure-stdlib half of agent_check.go — its
// CheckAgentProcess sibling shells out to a tmux server and walks
// /proc, both of which are sandbox-hostile. CheckWorkdir is fully
// unit-testable with t.TempDir.

func TestCheckWorkdir_Empty(t *testing.T) {
	r := CheckWorkdir("")
	if r.Passed() {
		t.Fatal("expected fail on empty workdir")
	}
	if r.Name != "workdir" {
		t.Errorf("Name = %q, want workdir", r.Name)
	}
	if !strings.Contains(r.Message, "not set") {
		t.Errorf("Message should mention not-set, got %q", r.Message)
	}
	if r.Fix == "" {
		t.Error("expected non-empty Fix hint for empty workdir")
	}
}

func TestCheckWorkdir_Missing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	r := CheckWorkdir(missing)
	if r.Passed() {
		t.Fatal("expected fail on missing dir")
	}
	if !strings.Contains(r.Message, "does not exist") {
		t.Errorf("Message should mention does-not-exist, got %q", r.Message)
	}
	if !strings.Contains(r.Fix, "mkdir") {
		t.Errorf("Fix should mention mkdir, got %q", r.Fix)
	}
}

func TestCheckWorkdir_FileNotDir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "afile")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	r := CheckWorkdir(file)
	if r.Passed() {
		t.Fatal("expected fail on file-as-workdir")
	}
	if !strings.Contains(r.Message, "not a directory") {
		t.Errorf("Message should mention not-a-directory, got %q", r.Message)
	}
}

func TestCheckWorkdir_Happy(t *testing.T) {
	dir := t.TempDir()
	r := CheckWorkdir(dir)
	if !r.Passed() {
		t.Fatalf("expected pass on real dir, got %+v", r)
	}
	if r.Status != StatusPass {
		t.Errorf("Status = %q, want %q", r.Status, StatusPass)
	}
	if !strings.Contains(r.Message, "exists and is a directory") {
		t.Errorf("Message should mention success, got %q", r.Message)
	}
}

// TestCheckWorkdir_StatError exercises the "stat returned an error
// that is not IsNotExist" branch. On Linux, attempting to stat a path
// under an unreadable directory yields EACCES rather than ENOENT.
//
// Skips on root (root can read everything).
func TestCheckWorkdir_StatError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root; cannot exercise permission-denied branch")
	}
	parent := filepath.Join(t.TempDir(), "locked")
	if err := os.Mkdir(parent, 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "child")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	// Strip parent's execute bit so stat on child fails with EACCES.
	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })

	r := CheckWorkdir(target)
	if r.Passed() {
		t.Fatal("expected fail when stat returns non-NotExist error")
	}
	if !strings.Contains(r.Message, "error checking") {
		t.Errorf("Message should mention stat error, got %q", r.Message)
	}
}
