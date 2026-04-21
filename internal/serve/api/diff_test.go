package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/serve/git"
)

const diffTestSHA = "3e17c87aabbccddee0011223344556677889900a"

// installDiffStubs wires the package-level seams (`checkpointsLister`
// and `diffFn`) to deterministic in-memory doubles for a single test.
// The prior values are restored via t.Cleanup.
func installDiffStubs(
	t *testing.T,
	lister func(workdir string, limit int) ([]git.Checkpoint, error),
	diff func(workdir, sha string) (string, error),
) {
	t.Helper()
	prevL := checkpointsLister
	prevD := diffFn
	t.Cleanup(func() {
		checkpointsLister = prevL
		diffFn = prevD
	})
	if lister != nil {
		checkpointsLister = lister
	}
	if diff != nil {
		diffFn = diff
	}
}

func newDiffRequest(t *testing.T, name, sha string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+name+"/checkpoints/"+sha+"/diff", nil)
	req.SetPathValue("name", name)
	req.SetPathValue("sha", sha)
	return req
}

func TestDiff_HappyPathReturnsPlainText(t *testing.T) {
	wantBody := "commit 3e17c87\ndiff --git a/x b/x\n+added line\n"
	installDiffStubs(t,
		func(workdir string, limit int) ([]git.Checkpoint, error) {
			return []git.Checkpoint{{SHA: diffTestSHA, Subject: "checkpoint: pre-yolo"}}, nil
		},
		func(workdir, sha string) (string, error) {
			if sha != diffTestSHA {
				t.Errorf("diffFn called with sha = %q, want %q", sha, diffTestSHA)
			}
			return wantBody, nil
		},
	)

	cache := NewCheckpointsCache()
	h := Diff(func(name string) (string, bool) { return "/wd", true }, cache)

	rec := httptest.NewRecorder()
	h(rec, newDiffRequest(t, "sess", diffTestSHA))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Errorf("content-type = %q, want text/plain prefix", ct)
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != wantBody {
		t.Errorf("body = %q, want %q", string(body), wantBody)
	}
}

func TestDiff_MalformedSHAReturns400(t *testing.T) {
	// No stubs needed — the 400 path short-circuits before lookup.
	cache := NewCheckpointsCache()
	h := Diff(func(name string) (string, bool) { return "/wd", true }, cache)

	cases := []string{
		"",
		"deadbeef",                                 // too short
		"DEADBEEFDEADBEEFDEADBEEFDEADBEEFDEADBEEF", // uppercase hex
		"zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", // non-hex
		diffTestSHA + "a",                          // too long
	}
	for _, sha := range cases {
		rec := httptest.NewRecorder()
		h(rec, newDiffRequest(t, "sess", sha))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("sha=%q: status = %d, want 400", sha, rec.Code)
		}
	}
}

func TestDiff_NonCheckpointSHAReturns404(t *testing.T) {
	// Lister returns a *different* SHA, so the requested one is not
	// in the allowlist and the handler must refuse.
	otherSHA := "0011223344556677889900aabbccddeeff001122"
	installDiffStubs(t,
		func(workdir string, limit int) ([]git.Checkpoint, error) {
			return []git.Checkpoint{{SHA: otherSHA, Subject: "checkpoint: x"}}, nil
		},
		func(workdir, sha string) (string, error) {
			t.Fatalf("diffFn must not be called for non-checkpoint SHA")
			return "", nil
		},
	)

	cache := NewCheckpointsCache()
	h := Diff(func(name string) (string, bool) { return "/wd", true }, cache)

	rec := httptest.NewRecorder()
	h(rec, newDiffRequest(t, "sess", diffTestSHA))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestDiff_UnknownSessionReturns404(t *testing.T) {
	// resolveWorkdir returns false → handler must 404 before touching
	// the cache or the diff seam.
	installDiffStubs(t,
		func(workdir string, limit int) ([]git.Checkpoint, error) {
			t.Fatalf("lister must not be called when session is unknown")
			return nil, nil
		},
		func(workdir, sha string) (string, error) {
			t.Fatalf("diffFn must not be called when session is unknown")
			return "", nil
		},
	)

	cache := NewCheckpointsCache()
	h := Diff(func(name string) (string, bool) { return "", false }, cache)

	rec := httptest.NewRecorder()
	h(rec, newDiffRequest(t, "missing", diffTestSHA))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestDiff_405OnPost(t *testing.T) {
	cache := NewCheckpointsCache()
	h := Diff(func(name string) (string, bool) { return "/wd", true }, cache)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/x/checkpoints/"+diffTestSHA+"/diff", nil)
	req.SetPathValue("name", "x")
	req.SetPathValue("sha", diffTestSHA)
	h(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestDiff_GitErrorReturns500(t *testing.T) {
	installDiffStubs(t,
		func(workdir string, limit int) ([]git.Checkpoint, error) {
			return []git.Checkpoint{{SHA: diffTestSHA, Subject: "checkpoint: ok"}}, nil
		},
		func(workdir, sha string) (string, error) {
			return "", &diffError{msg: "git show exploded"}
		},
	)

	cache := NewCheckpointsCache()
	h := Diff(func(name string) (string, bool) { return "/wd", true }, cache)

	rec := httptest.NewRecorder()
	h(rec, newDiffRequest(t, "sess", diffTestSHA))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
}

// diffError is a minimal error type for the 500-path test.
type diffError struct{ msg string }

func (e *diffError) Error() string { return e.msg }
