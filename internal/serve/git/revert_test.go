package git

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRevert_DirtyWithoutStash_ReturnsDirtyError(t *testing.T) {
	requireGit(t)
	dir := newRepo(t)
	commit(t, dir, "checkpoint: pre-yolo first")
	target := headSHA(t, dir)
	commit(t, dir, "feat: add stuff")

	// Make workdir dirty.
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatalf("dirty: %v", err)
	}

	_, err := Revert(dir, target, false)
	if err == nil {
		t.Fatal("want error, got nil")
	}
	var de *DirtyError
	if !errors.As(err, &de) {
		t.Fatalf("want *DirtyError, got %T: %v", err, err)
	}
	if len(de.Files) == 0 {
		t.Fatal("DirtyError.Files empty")
	}
	found := false
	for _, f := range de.Files {
		if f == "README" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("README not in dirty files: %v", de.Files)
	}
}

func TestRevert_DirtyWithStash_Succeeds(t *testing.T) {
	requireGit(t)
	dir := newRepo(t)
	commit(t, dir, "checkpoint: pre-yolo first")
	target := headSHA(t, dir)
	commit(t, dir, "feat: add stuff")

	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatalf("dirty: %v", err)
	}

	res, err := Revert(dir, target, true)
	if err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if !res.OK {
		t.Error("OK = false")
	}
	if res.RevertedTo != target {
		t.Errorf("RevertedTo = %q, want %q", res.RevertedTo, target)
	}
	if res.StashedAs == "" {
		t.Error("StashedAs empty after stash")
	}

	stashList := mustOut(t, dir, "git", "stash", "list")
	if !strings.Contains(stashList, "ctm-revert pre-") {
		t.Errorf("stash entry missing in list:\n%s", stashList)
	}

	if got := headSHA(t, dir); got != target {
		t.Errorf("HEAD = %s, want %s", got, target)
	}
}

func TestRevert_CleanWorkdir_Succeeds(t *testing.T) {
	requireGit(t)
	dir := newRepo(t)
	commit(t, dir, "checkpoint: pre-yolo first")
	target := headSHA(t, dir)
	commit(t, dir, "feat: add stuff")

	res, err := Revert(dir, target, false)
	if err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if !res.OK || res.RevertedTo != target {
		t.Errorf("unexpected result: %+v", res)
	}
	if res.StashedAs != "" {
		t.Errorf("StashedAs unexpectedly set: %q", res.StashedAs)
	}
	if got := headSHA(t, dir); got != target {
		t.Errorf("HEAD = %s, want %s", got, target)
	}
}

func TestRevert_NoGitDir_Errors(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()
	_, err := Revert(dir, "deadbeef", false)
	if err == nil {
		t.Fatal("want error for non-repo workdir")
	}
}

func TestRevert_EmptySHA_Errors(t *testing.T) {
	requireGit(t)
	dir := newRepo(t)
	if _, err := Revert(dir, "", false); err == nil {
		t.Fatal("want error for empty sha")
	}
}

func headSHA(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(mustOut(t, dir, "git", "rev-parse", "HEAD"))
}

func mustOut(t *testing.T, dir, name string, args ...string) string {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("%s %v: %v", name, args, err)
	}
	return string(out)
}
