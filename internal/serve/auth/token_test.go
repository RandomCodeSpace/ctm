package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureToken_GeneratesAndPersists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "serve.token")

	tok, err := EnsureToken(path)
	if err != nil {
		t.Fatalf("EnsureToken: %v", err)
	}
	if len(tok) < 40 {
		t.Errorf("token unexpectedly short: %d chars", len(tok))
	}
	if strings.ContainsAny(tok, "\n\r\t ") {
		t.Errorf("token contains whitespace: %q", tok)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("file mode = %v, want 0600", mode)
	}

	on_disk, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("readfile: %v", err)
	}
	if string(on_disk) != tok {
		t.Errorf("on-disk token mismatch: got %q want %q", on_disk, tok)
	}
}

func TestEnsureToken_RoundtripsExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "serve.token")

	first, err := EnsureToken(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := EnsureToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Errorf("EnsureToken not idempotent: first=%q second=%q", first, second)
	}
}

func TestEnsureToken_TrimsWhitespace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "serve.token")
	if err := os.WriteFile(path, []byte("  abc-def  \n"), 0600); err != nil {
		t.Fatal(err)
	}
	tok, err := EnsureToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if tok != "abc-def" {
		t.Errorf("got %q, want %q", tok, "abc-def")
	}
}

func TestEnsureToken_EmptyFileRejected(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "serve.token")
	if err := os.WriteFile(path, []byte("   \n\t"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureToken(path); err == nil {
		t.Fatal("expected error for empty token, got nil")
	}
}

func TestEnsureToken_MissingParentSurfaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "serve.token")
	if _, err := EnsureToken(path); err == nil {
		t.Fatal("expected error when parent directory missing, got nil")
	}
}

func TestLoadToken_MissingFileWrappedNotExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "absent.token")
	_, err := LoadToken(path)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("error not wrapping os.ErrNotExist: %v", err)
	}
}

func TestLoadToken_RejectsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "serve.token")
	if err := os.WriteFile(path, []byte("\n  \n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadToken(path); err == nil {
		t.Fatal("expected error for empty token, got nil")
	}
}

func TestLoadToken_HappyPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "serve.token")
	if err := os.WriteFile(path, []byte("hello-token\n"), 0600); err != nil {
		t.Fatal(err)
	}
	tok, err := LoadToken(path)
	if err != nil {
		t.Fatal(err)
	}
	if tok != "hello-token" {
		t.Errorf("got %q, want %q", tok, "hello-token")
	}
}

func TestTokenPath_PointsAtConfigDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	got := TokenPath()
	if !strings.HasSuffix(got, filepath.Join(".config", "ctm", "serve.token")) {
		t.Errorf("TokenPath = %q, want suffix .config/ctm/serve.token", got)
	}
}
