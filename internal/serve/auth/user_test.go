package auth_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/serve/auth"
)

func withTempHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	old := os.Getenv("HOME")
	t.Cleanup(func() { _ = os.Setenv("HOME", old) })
	_ = os.Setenv("HOME", home)
	return home
}

func TestUser_Exists_False_WhenNoFile(t *testing.T) {
	withTempHome(t)
	if auth.Exists() {
		t.Fatal("Exists() = true, want false on a fresh home")
	}
}

func TestUser_Save_Load_RoundTrip(t *testing.T) {
	home := withTempHome(t)
	enc, err := auth.Hash("pw")
	if err != nil {
		t.Fatal(err)
	}
	u := auth.User{Username: "alice", Password: enc}
	if err := auth.Save(u); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if !auth.Exists() {
		t.Fatal("Exists() = false after Save")
	}
	got, err := auth.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Username != "alice" {
		t.Fatalf("username = %q, want %q", got.Username, "alice")
	}
	if !auth.Verify(got.Password, "pw") {
		t.Fatal("Verify(stored, \"pw\") = false, want true")
	}
	// File is inside ~/.config/ctm/ and is 0600.
	p := filepath.Join(home, ".config", "ctm", "user.json")
	info, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat user.json: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("perm = %o, want 0600", info.Mode().Perm())
	}
}

func TestUser_Delete(t *testing.T) {
	withTempHome(t)
	enc, _ := auth.Hash("pw")
	if err := auth.Save(auth.User{Username: "alice", Password: enc}); err != nil {
		t.Fatal(err)
	}
	if err := auth.Delete(); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if auth.Exists() {
		t.Fatal("Exists() = true after Delete")
	}
	if err := auth.Delete(); err != nil {
		t.Fatalf("second Delete should be a no-op, got %v", err)
	}
}
