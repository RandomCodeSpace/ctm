package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/git"
)

// gitWorkdir returns a tempdir with a .git directory so the handler's
// isGitWorkdir check passes — otherwise the handler short-circuits
// before calling the lister.
func gitWorkdir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestCheckpoints_404OnUnknownSession(t *testing.T) {
	h := Checkpoints(func(name string) (string, bool) { return "", false }, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/missing/checkpoints", nil)
	h(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestCheckpoints_405OnPost(t *testing.T) {
	h := Checkpoints(func(name string) (string, bool) { return "/tmp", true }, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/x/checkpoints", nil)
	h(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestCheckpoints_CacheHitWithin5s(t *testing.T) {
	prev := checkpointsLister
	t.Cleanup(func() { checkpointsLister = prev })

	var calls int32
	want := []git.Checkpoint{{SHA: "abc", Subject: "checkpoint: pre-yolo x", TS: "2026-04-20T10:00:00Z", Ago: "2m"}}
	checkpointsLister = func(workdir string, limit int) ([]git.Checkpoint, error) {
		atomic.AddInt32(&calls, 1)
		return want, nil
	}

	wd := gitWorkdir(t)
	h := Checkpoints(func(name string) (string, bool) { return wd, true }, nil)

	for i := 0; i < 5; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/sessions/sess/checkpoints", nil)
		req.SetPathValue("name", "sess")
		h(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: status = %d", i, rec.Code)
		}
		var got checkpointsResp
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if !got.GitWorkdir {
			t.Fatalf("call %d: git_workdir = false, want true", i)
		}
		if len(got.Checkpoints) != 1 || got.Checkpoints[0].SHA != "abc" {
			t.Errorf("call %d: payload = %+v", i, got)
		}
	}
	if c := atomic.LoadInt32(&calls); c != 1 {
		t.Errorf("lister called %d times, want 1 (cached)", c)
	}
}

func TestCheckpoints_CacheKeyedOnLimit(t *testing.T) {
	prev := checkpointsLister
	t.Cleanup(func() { checkpointsLister = prev })

	var calls int32
	checkpointsLister = func(workdir string, limit int) ([]git.Checkpoint, error) {
		atomic.AddInt32(&calls, 1)
		return nil, nil
	}
	wd := gitWorkdir(t)
	h := Checkpoints(func(name string) (string, bool) { return wd, true }, nil)

	for _, q := range []string{"", "?limit=10", "?limit=10", "?limit=20"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/api/sessions/s/checkpoints"+q, nil)
		req.SetPathValue("name", "s")
		h(rec, req)
	}
	// Three distinct cache keys: default(50), 10, 20. Second "?limit=10" hits cache.
	if c := atomic.LoadInt32(&calls); c != 3 {
		t.Errorf("lister calls = %d, want 3", c)
	}
}

func TestCheckpoints_NilListEncodedAsEmptyArray(t *testing.T) {
	prev := checkpointsLister
	t.Cleanup(func() { checkpointsLister = prev })
	checkpointsLister = func(workdir string, limit int) ([]git.Checkpoint, error) {
		return nil, nil
	}
	wd := gitWorkdir(t)
	h := Checkpoints(func(name string) (string, bool) { return wd, true }, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/s/checkpoints", nil)
	req.SetPathValue("name", "s")
	h(rec, req)
	var got checkpointsResp
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.GitWorkdir {
		t.Fatalf("git_workdir = false, want true")
	}
	if got.Checkpoints == nil || len(got.Checkpoints) != 0 {
		t.Errorf("checkpoints = %+v, want empty non-nil slice", got.Checkpoints)
	}
}

func TestCheckpoints_NotGitWorkdir(t *testing.T) {
	// No .git dir — handler must short-circuit and return
	// git_workdir:false without calling the lister.
	prev := checkpointsLister
	t.Cleanup(func() { checkpointsLister = prev })
	var calls int32
	checkpointsLister = func(workdir string, limit int) ([]git.Checkpoint, error) {
		atomic.AddInt32(&calls, 1)
		return nil, nil
	}

	wd := t.TempDir() // no .git subdir
	h := Checkpoints(func(name string) (string, bool) { return wd, true }, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/s/checkpoints", nil)
	req.SetPathValue("name", "s")
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got checkpointsResp
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.GitWorkdir {
		t.Errorf("git_workdir = true, want false for non-git workdir")
	}
	if len(got.Checkpoints) != 0 {
		t.Errorf("checkpoints = %+v, want empty", got.Checkpoints)
	}
	if c := atomic.LoadInt32(&calls); c != 0 {
		t.Errorf("lister called %d times for non-git workdir, want 0", c)
	}
}

func TestCheckpointsCache_IsCheckpointFullSHAOnly(t *testing.T) {
	prev := checkpointsLister
	t.Cleanup(func() { checkpointsLister = prev })

	const fullSHA = "3e17c87aabbccddee0011223344556677889900"
	checkpointsLister = func(workdir string, limit int) ([]git.Checkpoint, error) {
		return []git.Checkpoint{{SHA: fullSHA, Subject: "checkpoint: pre-yolo 2026"}}, nil
	}

	cache := NewCheckpointsCache()
	if !cache.IsCheckpoint("/wd", "name", fullSHA) {
		t.Error("full SHA must be allowed")
	}
	if cache.IsCheckpoint("/wd", "name", fullSHA[:7]) {
		t.Error("7-char abbreviated SHA must be rejected")
	}
	if cache.IsCheckpoint("/wd", "name", fullSHA[:12]) {
		t.Error("12-char abbreviated SHA must be rejected")
	}
	if cache.IsCheckpoint("/wd", "name", "") {
		t.Error("empty SHA must be rejected")
	}
}

func TestCheckpoints_CacheExpiresAfterTTL(t *testing.T) {
	prev := checkpointsLister
	t.Cleanup(func() { checkpointsLister = prev })

	var calls int32
	checkpointsLister = func(workdir string, limit int) ([]git.Checkpoint, error) {
		atomic.AddInt32(&calls, 1)
		return nil, nil
	}
	wd := gitWorkdir(t)
	h := Checkpoints(func(name string) (string, bool) { return wd, true }, nil)

	rec1 := httptest.NewRecorder()
	r1 := httptest.NewRequest(http.MethodGet, "/api/sessions/s/checkpoints", nil)
	r1.SetPathValue("name", "s")
	h(rec1, r1)
	rec2 := httptest.NewRecorder()
	r2 := httptest.NewRequest(http.MethodGet, "/api/sessions/s/checkpoints", nil)
	r2.SetPathValue("name", "s")
	h(rec2, r2)
	if c := atomic.LoadInt32(&calls); c != 1 {
		t.Fatalf("cache miss within TTL: calls = %d", c)
	}
	_ = time.Now()
}
