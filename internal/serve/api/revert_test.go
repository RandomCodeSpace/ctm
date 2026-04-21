package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/serve/git"
)

func TestRevert_405OnGet(t *testing.T) {
	h := Revert(
		func(name string) (string, bool) { return "/wd", true },
		func(name, sha string) bool { return true },
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/s/revert", nil)
	req.SetPathValue("name", "s")
	h(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestRevert_404OnUnknownSession(t *testing.T) {
	h := Revert(
		func(name string) (string, bool) { return "", false },
		func(name, sha string) bool { return true },
	)
	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"sha":"deadbeef"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s/revert", body)
	req.SetPathValue("name", "s")
	h(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestRevert_400OnUnknownField(t *testing.T) {
	h := Revert(
		func(name string) (string, bool) { return "/wd", true },
		func(name, sha string) bool { return true },
	)
	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"sha":"a","danger":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s/revert", body)
	req.SetPathValue("name", "s")
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestRevert_400OnEmptyBody(t *testing.T) {
	h := Revert(
		func(name string) (string, bool) { return "/wd", true },
		func(name, sha string) bool { return true },
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s/revert", strings.NewReader(""))
	req.SetPathValue("name", "s")
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestRevert_422OnDisallowedSHA(t *testing.T) {
	h := Revert(
		func(name string) (string, bool) { return "/wd", true },
		func(name, sha string) bool { return false },
	)
	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"sha":"deadbeef"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s/revert", body)
	req.SetPathValue("name", "s")
	h(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
	var got map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&got)
	if got["error"] != "sha_not_a_checkpoint" {
		t.Errorf("error = %q, want sha_not_a_checkpoint", got["error"])
	}
}

func TestRevert_409OnDirtyWorkdir(t *testing.T) {
	prev := revertFn
	t.Cleanup(func() { revertFn = prev })
	revertFn = func(workdir, sha string, stashFirst bool) (git.RevertResult, error) {
		return git.RevertResult{}, &git.DirtyError{Files: []string{"README", "main.go"}}
	}

	h := Revert(
		func(name string) (string, bool) { return "/wd", true },
		func(name, sha string) bool { return true },
	)
	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"sha":"abc"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s/revert", body)
	req.SetPathValue("name", "s")
	h(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
	var got struct {
		Error      string   `json:"error"`
		DirtyFiles []string `json:"dirty_files"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Error != "dirty_workdir" {
		t.Errorf("error = %q, want dirty_workdir", got.Error)
	}
	if len(got.DirtyFiles) != 2 || got.DirtyFiles[0] != "README" {
		t.Errorf("dirty_files = %v", got.DirtyFiles)
	}
}

func TestRevert_200OnSuccess(t *testing.T) {
	prev := revertFn
	t.Cleanup(func() { revertFn = prev })
	revertFn = func(workdir, sha string, stashFirst bool) (git.RevertResult, error) {
		return git.RevertResult{OK: true, RevertedTo: sha, StashedAs: "stashSHA"}, nil
	}

	h := Revert(
		func(name string) (string, bool) { return "/wd", true },
		func(name, sha string) bool { return true },
	)
	rec := httptest.NewRecorder()
	body := strings.NewReader(`{"sha":"abc","stash_first":true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s/revert", body)
	req.SetPathValue("name", "s")
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got git.RevertResult
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.OK || got.RevertedTo != "abc" || got.StashedAs != "stashSHA" {
		t.Errorf("result = %+v", got)
	}
}

func TestRevert_500OnGenericError(t *testing.T) {
	prev := revertFn
	t.Cleanup(func() { revertFn = prev })
	revertFn = func(workdir, sha string, stashFirst bool) (git.RevertResult, error) {
		return git.RevertResult{}, errStub("git reset --hard abc: exit 128: bad object")
	}
	h := Revert(
		func(name string) (string, bool) { return "/wd", true },
		func(name, sha string) bool { return true },
	)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s/revert", strings.NewReader(`{"sha":"abc"}`))
	req.SetPathValue("name", "s")
	h(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	var got map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&got)
	// sanitiseErr drops everything past the first colon — should not
	// leak "bad object" or the SHA-bearing path.
	if strings.Contains(got["error"], "bad object") {
		t.Errorf("error leaked stderr: %q", got["error"])
	}
}

type errStub string

func (e errStub) Error() string { return string(e) }
