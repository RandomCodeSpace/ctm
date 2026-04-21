package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeResolver implements UUIDNameResolver with an in-memory map.
type fakeResolver struct{ m map[string]string }

func (f fakeResolver) ResolveUUID(uuid string) (string, bool) {
	n, ok := f.m[uuid]
	return n, ok
}

// seedLogs writes len(sizes) *.jsonl files of exactly the requested
// byte length into dir. Returns the UUID slice in the same order.
func seedLogs(t *testing.T, dir string, sizes map[string]int) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for uuid, n := range sizes {
		path := filepath.Join(dir, uuid+".jsonl")
		if err := os.WriteFile(path, []byte(strings.Repeat("x", n)), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
}

func TestLogsUsage_405OnPost(t *testing.T) {
	h := LogsUsage(t.TempDir(), nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/logs/usage", nil)
	h(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); !strings.Contains(got, "GET") {
		t.Errorf("Allow header = %q, want GET", got)
	}
}

func TestLogsUsage_MissingDirReturnsEmpty(t *testing.T) {
	// Point at a dir that doesn't exist — serve startup creates it on
	// demand, but /api/logs/usage must not 5xx before that happens.
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	h := LogsUsage(missing, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/logs/usage", nil)
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body logsUsageResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Dir != missing {
		t.Errorf("dir = %q, want %q", body.Dir, missing)
	}
	if body.TotalBytes != 0 {
		t.Errorf("total_bytes = %d, want 0", body.TotalBytes)
	}
	if body.Files == nil {
		t.Error("files = nil, want empty slice (JSON [] not null)")
	}
	if len(body.Files) != 0 {
		t.Errorf("files = %+v, want empty", body.Files)
	}
}

func TestLogsUsage_JSONShapeAndTotals(t *testing.T) {
	dir := t.TempDir()
	sizes := map[string]int{
		"aaaaaaaa-0000-0000-0000-000000000001": 100,
		"bbbbbbbb-0000-0000-0000-000000000002": 250,
		"cccccccc-0000-0000-0000-000000000003": 50,
	}
	seedLogs(t, dir, sizes)
	// A non-jsonl file must be ignored entirely (no stat, no row).
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("ignore me"), 0o644); err != nil {
		t.Fatalf("seed README: %v", err)
	}
	// A subdir must also be ignored.
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}

	resolver := fakeResolver{m: map[string]string{
		"aaaaaaaa-0000-0000-0000-000000000001": "alpha",
		"bbbbbbbb-0000-0000-0000-000000000002": "beta",
		// cccccccc... intentionally unresolved → uuid:<short> fallback.
	}}
	h := LogsUsage(dir, resolver)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/logs/usage", nil)
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}

	var body logsUsageResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Dir != dir {
		t.Errorf("dir = %q, want %q", body.Dir, dir)
	}
	if body.TotalBytes != 400 {
		t.Errorf("total_bytes = %d, want 400 (100+250+50)", body.TotalBytes)
	}
	if len(body.Files) != 3 {
		t.Fatalf("files count = %d, want 3: %+v", len(body.Files), body.Files)
	}

	// Files must be sorted by bytes desc: beta(250), alpha(100), ccc(50).
	if body.Files[0].Session != "beta" || body.Files[0].Bytes != 250 {
		t.Errorf("files[0] = %+v, want {session:beta bytes:250}", body.Files[0])
	}
	if body.Files[1].Session != "alpha" || body.Files[1].Bytes != 100 {
		t.Errorf("files[1] = %+v, want {session:alpha bytes:100}", body.Files[1])
	}
	if !strings.HasPrefix(body.Files[2].Session, "uuid:") {
		t.Errorf("files[2].session = %q, want uuid:<short> fallback", body.Files[2].Session)
	}
	if body.Files[2].Bytes != 50 {
		t.Errorf("files[2].bytes = %d, want 50", body.Files[2].Bytes)
	}

	// UUID propagated through verbatim.
	for _, f := range body.Files {
		if _, ok := sizes[f.UUID]; !ok {
			t.Errorf("unexpected uuid %q", f.UUID)
		}
		if f.Mtime == "" {
			t.Errorf("file %s mtime empty", f.UUID)
		}
	}

	// Shape check: top-level keys exactly {dir, total_bytes, files}.
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err == nil {
		for _, key := range []string{"dir", "total_bytes", "files"} {
			if _, ok := raw[key]; !ok {
				t.Errorf("missing top-level key %q", key)
			}
		}
	}
}

func TestLogsUsage_NilResolverAlwaysFallsBack(t *testing.T) {
	dir := t.TempDir()
	seedLogs(t, dir, map[string]int{
		"12345678-aaaa-bbbb-cccc-000000000000": 10,
	})
	h := LogsUsage(dir, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/logs/usage", nil)
	h(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body logsUsageResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Files) != 1 {
		t.Fatalf("files = %+v", body.Files)
	}
	if body.Files[0].Session != "uuid:12345678" {
		t.Errorf("session = %q, want uuid:12345678", body.Files[0].Session)
	}
}

func TestLogsUsage_507OverLimit(t *testing.T) {
	dir := t.TempDir()
	// Seed just over the bound. Use tiny files — we only care about the
	// count gate, not byte totals.
	sizes := make(map[string]int, maxFilesLimit+1)
	for i := 0; i <= maxFilesLimit; i++ {
		sizes[fmt.Sprintf("uuid-%08d", i)] = 1
	}
	seedLogs(t, dir, sizes)

	h := LogsUsage(dir, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/logs/usage", nil)
	h(rec, req)

	if rec.Code != http.StatusInsufficientStorage {
		t.Fatalf("status = %d, want 507", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] != "too_many_log_files" {
		t.Errorf("error = %v, want too_many_log_files", body["error"])
	}
	if _, ok := body["hint"]; !ok {
		t.Errorf("missing hint field in 507 body")
	}
}
