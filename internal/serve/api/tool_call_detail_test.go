package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeLogReader is the testing seam for ToolCallDetail. Returns canned
// Details keyed on (session, id); any key not present reports
// ErrDetailNotFound so the handler's 404 path can be exercised.
type fakeLogReader struct {
	items map[string]Detail
	err   error
}

func (f fakeLogReader) ReadDetail(session, id string) (Detail, error) {
	if f.err != nil {
		return Detail{}, f.err
	}
	if d, ok := f.items[session+"|"+id]; ok {
		return d, nil
	}
	return Detail{}, ErrDetailNotFound
}

func newDetailRequest(t *testing.T, session, id string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/sessions/"+session+"/tool_calls/"+id+"/detail",
		nil,
	)
	req.SetPathValue("name", session)
	req.SetPathValue("id", id)
	return req
}

func TestToolCallDetail_HappyPathReturnsJSON(t *testing.T) {
	want := Detail{
		Tool:          "Edit",
		InputJSON:     `{"file_path":"/tmp/a.go","old_string":"foo","new_string":"bar"}`,
		OutputExcerpt: "ok",
		TS:            "2026-04-21T16:28:00Z",
		IsError:       false,
		Diff:          "--- a/tmp/a.go\n+++ b/tmp/a.go\n@@ -1,1 +1,1 @@\n-foo\n+bar\n",
	}
	h := ToolCallDetail(fakeLogReader{
		items: map[string]Detail{"alpha|17771234-0": want},
	})
	rec := httptest.NewRecorder()
	h(rec, newDetailRequest(t, "alpha", "17771234-0"))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	var got Detail
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != want {
		t.Errorf("body mismatch\n got=%+v\nwant=%+v", got, want)
	}
}

func TestToolCallDetail_NotFoundReturns404(t *testing.T) {
	h := ToolCallDetail(fakeLogReader{items: map[string]Detail{}})
	rec := httptest.NewRecorder()
	h(rec, newDetailRequest(t, "alpha", "doesnotexist-0"))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

func TestToolCallDetail_MethodNotGet(t *testing.T) {
	h := ToolCallDetail(fakeLogReader{})
	for _, method := range []string{
		http.MethodPost,
		http.MethodPut,
		http.MethodDelete,
		http.MethodPatch,
	} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(
			method,
			"/api/sessions/alpha/tool_calls/1-0/detail",
			nil,
		)
		req.SetPathValue("name", "alpha")
		req.SetPathValue("id", "1-0")
		h(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", method, rec.Code)
		}
		if got := rec.Header().Get("Allow"); got != http.MethodGet {
			t.Errorf("%s: Allow = %q, want GET", method, got)
		}
	}
}

func TestToolCallDetail_EmptyPathVarsReturn400(t *testing.T) {
	h := ToolCallDetail(fakeLogReader{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions//tool_calls//detail", nil)
	req.SetPathValue("name", "")
	req.SetPathValue("id", "")
	h(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// renderDiff unit tests — exercise the three tool shapes. These hit
// the rendering directly rather than going through the HTTP layer so
// failures pinpoint the diff builder.

func TestRenderDiff_EditEmitsReplaceHunk(t *testing.T) {
	raw := map[string]any{
		"tool_name": "Edit",
		"tool_input": map[string]any{
			"file_path":  "/tmp/a.go",
			"old_string": "package foo\n\nfunc Bar() {}",
			"new_string": "package foo\n\nfunc Baz() {}",
		},
	}
	got := renderDiff("Edit", raw)
	mustContain(t, got, "--- a//tmp/a.go")
	mustContain(t, got, "+++ b//tmp/a.go")
	mustContain(t, got, "@@ -1,3 +1,3 @@")
	mustContain(t, got, "-func Bar() {}")
	mustContain(t, got, "+func Baz() {}")
}

func TestRenderDiff_WriteEmitsAllAddedHunk(t *testing.T) {
	raw := map[string]any{
		"tool_name": "Write",
		"tool_input": map[string]any{
			"file_path": "/tmp/new.txt",
			"content":   "hello\nworld\n",
		},
	}
	got := renderDiff("Write", raw)
	mustContain(t, got, "@@ -0,0 +1,2 @@")
	mustContain(t, got, "+hello")
	mustContain(t, got, "+world")
	if strings.Contains(got, "-") {
		// "-" would indicate a removed-line leak into an all-added
		// hunk. Check a position-aware pattern: "\n-" at the start
		// of a line is the problem shape.
		if strings.Contains(got, "\n-") {
			t.Errorf("Write diff must contain no removed lines:\n%s", got)
		}
	}
}

func TestRenderDiff_MultiEditEmitsMultipleHunks(t *testing.T) {
	raw := map[string]any{
		"tool_name": "MultiEdit",
		"tool_input": map[string]any{
			"file_path": "/tmp/multi.go",
			"edits": []any{
				map[string]any{
					"old_string": "alpha",
					"new_string": "ALPHA",
				},
				map[string]any{
					"old_string": "beta",
					"new_string": "BETA",
				},
			},
		},
	}
	got := renderDiff("MultiEdit", raw)
	hunks := strings.Count(got, "@@ -")
	if hunks != 2 {
		t.Errorf("hunk count = %d, want 2\n%s", hunks, got)
	}
	for _, token := range []string{"-alpha", "+ALPHA", "-beta", "+BETA"} {
		mustContain(t, got, token)
	}
}

func TestRenderDiff_NonDiffToolReturnsEmpty(t *testing.T) {
	raw := map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": "ls"},
	}
	if got := renderDiff("Bash", raw); got != "" {
		t.Errorf("Bash diff should be empty, got:\n%s", got)
	}
	if got := renderDiff("Read", raw); got != "" {
		t.Errorf("Read diff should be empty, got:\n%s", got)
	}
}

// JSONLLogReader integration — drives the real scan path against a
// hand-authored tailer-shaped JSONL file. Kept lean: one session, one
// matching line, one distractor line.

func TestJSONLLogReader_FindsMatchByTS(t *testing.T) {
	dir := t.TempDir()
	uuid := "11111111-2222-3333-4444-555555555555"
	path := filepath.Join(dir, uuid+".jsonl")

	ts := time.Date(2026, 4, 21, 16, 28, 0, 0, time.UTC)
	// Distractor: older line that should NOT match because its ts is
	// >1 s away from the target.
	older := map[string]any{
		"tool_name":     "Bash",
		"tool_input":    map[string]any{"command": "ls"},
		"ctm_timestamp": ts.Add(-10 * time.Second).Format(time.RFC3339),
	}
	// Target: Edit call right at the target second.
	target := map[string]any{
		"tool_name": "Edit",
		"tool_input": map[string]any{
			"file_path":  "/tmp/x.go",
			"old_string": "foo",
			"new_string": "bar",
		},
		"tool_response": map[string]any{
			"output":   "ok",
			"is_error": false,
		},
		"ctm_timestamp": ts.Format(time.RFC3339),
	}
	writeJSONLLine(t, path, older)
	writeJSONLLine(t, path, target)

	reader := &JSONLLogReader{
		LogDir:   dir,
		Resolver: staticResolver{"alpha": uuid},
	}
	// id = "<unix-nano>-<seq>"; nanos ÷ 1e9 == ts.Unix().
	id := toID(ts)
	d, err := reader.ReadDetail("alpha", id)
	if err != nil {
		t.Fatalf("ReadDetail: %v", err)
	}
	if d.Tool != "Edit" {
		t.Errorf("Tool = %q, want Edit", d.Tool)
	}
	if !strings.Contains(d.Diff, "-foo") || !strings.Contains(d.Diff, "+bar") {
		t.Errorf("Diff missing replace content:\n%s", d.Diff)
	}
	if d.TS != ts.Format(time.RFC3339) {
		t.Errorf("TS = %q, want %q", d.TS, ts.Format(time.RFC3339))
	}
}

func TestJSONLLogReader_MissingFileIsNotFound(t *testing.T) {
	dir := t.TempDir()
	reader := &JSONLLogReader{
		LogDir: dir,
		Resolver: staticResolver{
			"alpha": "99999999-9999-9999-9999-999999999999",
		},
	}
	_, err := reader.ReadDetail("alpha", toID(time.Now()))
	if err != ErrDetailNotFound {
		t.Errorf("err = %v, want ErrDetailNotFound", err)
	}
}

func TestJSONLLogReader_UnknownSessionIsNotFound(t *testing.T) {
	reader := &JSONLLogReader{
		LogDir:   t.TempDir(),
		Resolver: staticResolver{},
	}
	_, err := reader.ReadDetail("nope", toID(time.Now()))
	if err != ErrDetailNotFound {
		t.Errorf("err = %v, want ErrDetailNotFound", err)
	}
}

// --- helpers ---------------------------------------------------------

type staticResolver map[string]string

func (s staticResolver) ResolveName(name string) (string, bool) {
	u, ok := s[name]
	return u, ok
}

func writeJSONLLine(t *testing.T, path string, payload map[string]any) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := f.Write(append(body, '\n')); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// toID mirrors the hub's ID format "<unix-nano>-<seq>" for a given
// wall clock. Seq is always 0 here — we only match on the nanosecond
// prefix.
func toID(t time.Time) string {
	return timeNanoString(t) + "-0"
}

func timeNanoString(t time.Time) string {
	return strings0Pad(t.UnixNano())
}

// strings0Pad formats an int64 as decimal without thousand separators.
// Named instead of calling strconv directly so the helper file stays
// import-lean — the production path already imports strconv.
func strings0Pad(n int64) string {
	// Local FormatInt avoids a second strconv import line here.
	return fmtInt(n)
}

func fmtInt(n int64) string {
	// Simple wrapper around strconv.FormatInt via json.
	b, _ := json.Marshal(n)
	return string(b)
}

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Errorf("missing %q in:\n%s", needle, haystack)
	}
}
