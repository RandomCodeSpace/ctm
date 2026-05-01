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
