package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestList_ReturnsCheckpointsAndIgnoresOthers(t *testing.T) {
	requireGit(t)
	dir := newRepo(t)

	// Two checkpoints + one regular commit.
	commit(t, dir, "checkpoint: pre-yolo 2026-04-20T10:00:00")
	commit(t, dir, "feat: regular work")
	commit(t, dir, "checkpoint: pre-yolo 2026-04-20T11:00:00")

	got, err := List(dir, 50)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 checkpoints, got %d: %+v", len(got), got)
	}
	for _, c := range got {
		if !strings.HasPrefix(c.Subject, "checkpoint:") {
			t.Errorf("subject not a checkpoint: %q", c.Subject)
		}
		if c.SHA == "" {
			t.Error("empty SHA")
		}
		if _, perr := time.Parse(time.RFC3339, c.TS); perr != nil {
			t.Errorf("TS not RFC3339: %q (%v)", c.TS, perr)
		}
		if c.Ago == "" {
			t.Error("empty Ago")
		}
	}
}

func TestList_RespectsLimit(t *testing.T) {
	requireGit(t)
	dir := newRepo(t)
	for i := 0; i < 5; i++ {
		commit(t, dir, "checkpoint: pre-yolo n="+itoa(i))
	}
	got, err := List(dir, 3)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
}

func TestList_CapsAtMaxLimit(t *testing.T) {
	requireGit(t)
	dir := newRepo(t)
	commit(t, dir, "checkpoint: only one")

	// Asking for more than maxLimit should not error.
	got, err := List(dir, 10_000)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
}

func TestList_MissingWorkdirReturnsEmpty(t *testing.T) {
	got, err := List("/nonexistent/path/should/not/exist", 50)
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got != nil {
		t.Fatalf("want nil list, got %+v", got)
	}
}

func TestList_NoGitDirReturnsEmpty(t *testing.T) {
	dir := t.TempDir() // not a git repo
	got, err := List(dir, 50)
	if err != nil {
		t.Fatalf("want nil error, got %v", err)
	}
	if got != nil {
		t.Fatalf("want nil list, got %+v", got)
	}
}

func TestHumaniseAgo(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "0s"},
		{30 * time.Second, "30s"},
		{2 * time.Minute, "2m"},
		{3 * time.Hour, "3h"},
		{2 * 24 * time.Hour, "2d"},
		{2 * 7 * 24 * time.Hour, "2w"},
		{60 * 24 * time.Hour, "2mo"},
		{2 * 365 * 24 * time.Hour, "2y"},
		{-1 * time.Second, "0s"},
	}
	for _, c := range cases {
		if got := humaniseAgo(c.d); got != c.want {
			t.Errorf("humaniseAgo(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

// Helpers ---------------------------------------------------------

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH; skipping")
	}
}

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run(t, dir, "git", "init", "-q", "-b", "main")
	run(t, dir, "git", "config", "user.email", "test@example.com")
	run(t, dir, "git", "config", "user.name", "Test")
	run(t, dir, "git", "config", "commit.gpgsign", "false")
	// Seed with a regular commit so HEAD exists.
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	run(t, dir, "git", "add", "-A")
	run(t, dir, "git", "commit", "-q", "-m", "initial")
	return dir
}

func commit(t *testing.T, dir, msg string) {
	t.Helper()
	run(t, dir, "git", "commit", "-q", "--allow-empty", "-m", msg)
}

func run(t *testing.T, dir string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

func itoa(i int) string {
	return strconv.Itoa(i)
}
