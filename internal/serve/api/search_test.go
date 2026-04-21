package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func seedSearchLogs(t *testing.T, dir string, files map[string][]string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for name, lines := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
}

func TestSearch_FindsSubstringMatchesWithSnippet(t *testing.T) {
	dir := t.TempDir()
	seedSearchLogs(t, dir, map[string][]string{
		"11111111-0000-0000-0000-000000000001.jsonl": {
			`{"ts":"2026-04-21T10:00:00Z","tool":"Bash","cmd":"echo hello-needle-world"}`,
			`{"ts":"2026-04-21T10:00:01Z","tool":"Read","path":"/tmp/foo"}`,
		},
		"22222222-0000-0000-0000-000000000002.jsonl": {
			`{"ts":"2026-04-21T11:00:00Z","tool":"Grep","pattern":"needle"}`,
		},
	})
	resolver := fakeResolver{m: map[string]string{
		"11111111-0000-0000-0000-000000000001": "alpha",
		"22222222-0000-0000-0000-000000000002": "beta",
	}}

	h := Search(dir, resolver)
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=needle", nil)
	rr := httptest.NewRecorder()
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp SearchResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Query != "needle" {
		t.Errorf("query=%q want needle", resp.Query)
	}
	if resp.ScannedFiles != 2 {
		t.Errorf("scanned_files=%d want 2", resp.ScannedFiles)
	}
	if len(resp.Matches) != 2 {
		t.Fatalf("matches=%d want 2 (%v)", len(resp.Matches), resp.Matches)
	}
	// Snippet should contain the substring.
	for _, m := range resp.Matches {
		if !strings.Contains(m.Snippet, "needle") {
			t.Errorf("snippet %q missing query", m.Snippet)
		}
		if m.UUID == "" {
			t.Errorf("uuid empty")
		}
	}
}

func TestSearch_SessionFilter(t *testing.T) {
	dir := t.TempDir()
	seedSearchLogs(t, dir, map[string][]string{
		"aaaaaaaa-0000-0000-0000-000000000001.jsonl": {
			`{"ts":"2026-04-21T10:00:00Z","tool":"Bash","cmd":"needle-one"}`,
		},
		"bbbbbbbb-0000-0000-0000-000000000002.jsonl": {
			`{"ts":"2026-04-21T11:00:00Z","tool":"Bash","cmd":"needle-two"}`,
		},
	})
	resolver := fakeResolver{m: map[string]string{
		"aaaaaaaa-0000-0000-0000-000000000001": "alpha",
		"bbbbbbbb-0000-0000-0000-000000000002": "beta",
	}}
	h := Search(dir, resolver)

	req := httptest.NewRequest(http.MethodGet, "/api/search?q=needle&session=beta", nil)
	rr := httptest.NewRecorder()
	h(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	var resp SearchResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if resp.ScannedFiles != 1 {
		t.Errorf("scanned_files=%d want 1 (session filter)", resp.ScannedFiles)
	}
	if len(resp.Matches) != 1 || resp.Matches[0].Session != "beta" {
		t.Errorf("matches=%v want 1 from beta", resp.Matches)
	}
}

func TestSearch_TruncationFlag(t *testing.T) {
	dir := t.TempDir()
	lines := make([]string, 0, 10)
	for i := 0; i < 10; i++ {
		lines = append(lines, `{"ts":"2026-04-21T10:00:00Z","tool":"Bash","cmd":"has-needle-row"}`)
	}
	seedSearchLogs(t, dir, map[string][]string{
		"cccccccc-0000-0000-0000-000000000003.jsonl": lines,
	})
	h := Search(dir, fakeResolver{m: map[string]string{"cccccccc-0000-0000-0000-000000000003": "gamma"}})
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=needle&limit=3", nil)
	rr := httptest.NewRecorder()
	h(rr, req)

	var resp SearchResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Matches) != 3 {
		t.Errorf("matches=%d want 3", len(resp.Matches))
	}
	if !resp.Truncated {
		t.Errorf("truncated=false want true")
	}
}

func TestSearch_BadQuery400(t *testing.T) {
	h := Search(t.TempDir(), nil)
	for _, q := range []string{"", "x"} {
		req := httptest.NewRequest(http.MethodGet, "/api/search?q="+q, nil)
		rr := httptest.NewRecorder()
		h(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("q=%q status=%d want 400", q, rr.Code)
		}
	}
}

func TestSearch_MissingDirReturnsEmpty(t *testing.T) {
	h := Search(filepath.Join(t.TempDir(), "does-not-exist"), nil)
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=needle", nil)
	rr := httptest.NewRecorder()
	h(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var resp SearchResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if len(resp.Matches) != 0 || resp.ScannedFiles != 0 {
		t.Errorf("expected empty response, got %+v", resp)
	}
}

func TestSearch_405OnPost(t *testing.T) {
	h := Search(t.TempDir(), nil)
	req := httptest.NewRequest(http.MethodPost, "/api/search?q=needle", nil)
	rr := httptest.NewRecorder()
	h(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status=%d want 405", rr.Code)
	}
	if rr.Header().Get("Allow") != "GET" {
		t.Errorf("Allow=%q want GET", rr.Header().Get("Allow"))
	}
}
