package fsutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFile_CreatesNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	if err := AtomicWriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("content = %q, want %q", got, "hello")
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o644); got != want {
		t.Errorf("mode = %v, want %v", got, want)
	}
}

func TestAtomicWriteFile_OverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")

	if err := os.WriteFile(path, []byte("old contents"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := AtomicWriteFile(path, []byte("new"), 0o600); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "new" {
		t.Errorf("content = %q, want %q", got, "new")
	}

	// Forcing perm to 0o600 should win over the 0o644 the file was
	// originally created with.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got, want := info.Mode().Perm(), os.FileMode(0o600); got != want {
		t.Errorf("mode = %v, want %v", got, want)
	}
}

func TestAtomicWriteFile_FailsWhenDirMissing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-dir", "f.txt")
	err := AtomicWriteFile(missing, []byte("x"), 0o644)
	if err == nil {
		t.Fatalf("expected error writing into missing parent dir, got nil")
	}
}

func TestAtomicWriteFile_RenameOntoDirectoryFails(t *testing.T) {
	// rename(2) refuses to replace a non-empty directory with a regular
	// file (EISDIR / ENOTDIR). Pre-create a directory at the target path
	// so the temp-file write/chmod/close all succeed but Rename returns
	// an error — exercises the final error branch.
	dir := t.TempDir()
	target := filepath.Join(dir, "is-a-dir")
	if err := os.MkdirAll(filepath.Join(target, "child"), 0o755); err != nil {
		t.Fatalf("seed dir: %v", err)
	}

	err := AtomicWriteFile(target, []byte("payload"), 0o644)
	if err == nil {
		t.Fatalf("expected rename error when target is a non-empty directory, got nil")
	}

	// And the directory must still exist untouched.
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("Stat after failed rename: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected target to remain a directory")
	}
}

func TestAtomicWriteFile_PermissionsPropagated(t *testing.T) {
	// Goes beyond TestAtomicWriteFile_OverwritesExisting by sweeping a
	// few common modes — ensures the explicit Chmod call to override the
	// default 0600 from os.CreateTemp covers each.
	cases := []os.FileMode{0o600, 0o640, 0o644, 0o664, 0o755}
	dir := t.TempDir()
	for _, perm := range cases {
		path := filepath.Join(dir, perm.String()+".txt")
		if err := AtomicWriteFile(path, []byte("x"), perm); err != nil {
			t.Fatalf("AtomicWriteFile(perm=%v): %v", perm, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if got := info.Mode().Perm(); got != perm {
			t.Errorf("perm = %v, want %v", got, perm)
		}
	}
}

func TestAtomicWriteFile_NoTempFileLeftBehind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "real.txt")

	if err := AtomicWriteFile(path, []byte("payload"), 0o644); err != nil {
		t.Fatalf("AtomicWriteFile: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("expected only the renamed file, got: %v", names)
	}
}
