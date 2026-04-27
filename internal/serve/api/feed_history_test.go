package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// newHistoryRequest constructs a GET /api/sessions/{name}/feed/history
// test request with the Go 1.22 ServeMux path-value populated so the
// handler's r.PathValue("name") call resolves.
func newHistoryRequest(name, query string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+name+"/feed/history?"+query, nil)
	req.SetPathValue("name", name)
	return req
}

// writeJSONLFixture writes n tool_call hook-payload lines to <dir>/<uuid>.jsonl
// with monotonically increasing ctm_timestamps starting at `base`.
// Returns the slice of timestamps (nanos) written so tests can derive
// the expected ids.
func writeJSONLFixture(t *testing.T, dir, uuid string, n int, base time.Time) []int64 {
	t.Helper()
	path := filepath.Join(dir, uuid+".jsonl")
	fh, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer fh.Close()
	nanos := make([]int64, 0, n)
	for i := 0; i < n; i++ {
		ts := base.Add(time.Duration(i) * time.Second)
		// Force UTC + seconds precision for cursor determinism.
		tsStr := ts.UTC().Format(time.RFC3339)
		parsed, _ := time.Parse(time.RFC3339, tsStr)
		nanos = append(nanos, parsed.UnixNano())
		line := map[string]any{
			"tool_name": "Bash",
			"tool_input": map[string]any{
				"command": fmt.Sprintf("echo %d", i),
			},
			"tool_response": map[string]any{
				"output":   fmt.Sprintf("output-%d", i),
				"is_error": false,
			},
			"ctm_timestamp": tsStr,
		}
		enc, _ := json.Marshal(line)
		if _, err := fh.Write(append(enc, '\n')); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	return nanos
}

// historyResolver is a fake UUIDNameResolver pinned to a single mapping
// so tests can drive name→uuid resolution deterministically.
type historyResolver struct{ uuid, name string }

func (h historyResolver) ResolveUUID(u string) (string, bool) {
	if u == h.uuid {
		return h.name, true
	}
	return "", false
}

// projectionResolver implements both ResolveUUID (workdir-fallback
// semantics: every uuid reverse-maps to `name`) and ResolveName (the
// authoritative direct lookup). Used by TestResolveNameToUUID_Prefers
// ProjectionOverLexicalScan to reproduce the codeiq-style bug where
// a lexically-earlier dead log file shadowed the live one.
type projectionResolver struct {
	liveUUID string
	name     string
}

func (p projectionResolver) ResolveUUID(u string) (string, bool) { return p.name, true }
func (p projectionResolver) ResolveName(n string) (string, bool) {
	if n == p.name {
		return p.liveUUID, true
	}
	return "", false
}

func TestResolveNameToUUID_PrefersProjectionOverLexicalScan(t *testing.T) {
	// Two log files under logDir. deadUUID sorts lexically before
	// liveUUID; both reverse-map to "codeiq" via the workdir fallback
	// (projectionResolver.ResolveUUID returns "codeiq" for any input).
	// Without the direct-name lookup, resolveNameToUUID would return
	// deadUUID and callers (Subagents, Teams, FeedHistory) would open
	// the wrong file.
	const (
		deadUUID = "11111111-0000-0000-0000-000000000000"
		liveUUID = "99999999-0000-0000-0000-000000000000"
	)
	dir := t.TempDir()
	for _, u := range []string{deadUUID, liveUUID} {
		if err := os.WriteFile(filepath.Join(dir, u+".jsonl"), []byte{}, 0o600); err != nil {
			t.Fatalf("create %s: %v", u, err)
		}
	}

	got, ok := resolveNameToUUID(projectionResolver{liveUUID: liveUUID, name: "codeiq"}, dir, "codeiq")
	if !ok {
		t.Fatalf("resolveNameToUUID: ok=false, want true")
	}
	if got != liveUUID {
		t.Errorf("resolveNameToUUID = %q, want %q (projection/live uuid, not the lexically-earlier dead file)", got, liveUUID)
	}
}

func TestResolveNameToUUID_FallsBackToScanWhenNoDirectLookup(t *testing.T) {
	// historyResolver only implements ResolveUUID — no direct name
	// lookup — so the scan path must still work for orphan UUIDs /
	// legacy callers.
	const uuid = "aaaaaaaa-0000-0000-0000-000000000001"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, uuid+".jsonl"), []byte{}, 0o600); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, ok := resolveNameToUUID(historyResolver{uuid: uuid, name: "alpha"}, dir, "alpha")
	if !ok {
		t.Fatalf("resolveNameToUUID: ok=false, want true")
	}
	if got != uuid {
		t.Errorf("resolveNameToUUID = %q, want %q", got, uuid)
	}
}

func TestFeedHistory_BeforeInMiddleReturnsOlder(t *testing.T) {
	dir := t.TempDir()
	const uuid = "aaaaaaaa-0000-0000-0000-000000000001"
	base := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	nanos := writeJSONLFixture(t, dir, uuid, 50, base)

	h := FeedHistory(dir, historyResolver{uuid: uuid, name: "alpha"})

	// Cursor = id of the 30th event (0-indexed → index 30). Expect
	// events 0..29 returned, newest-first (29 down to 0).
	cursor := strconv.FormatInt(nanos[30], 10) + "-0"
	rec := httptest.NewRecorder()
	req := newHistoryRequest("alpha", "before="+cursor+"&limit=100")
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var body feedHistoryResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Events) != 30 {
		t.Fatalf("events = %d, want 30", len(body.Events))
	}
	// Newest-first: first returned must be event 29.
	wantFirst := strconv.FormatInt(nanos[29], 10) + "-0"
	if body.Events[0].ID != wantFirst {
		t.Errorf("events[0].id = %q, want %q", body.Events[0].ID, wantFirst)
	}
	wantLast := strconv.FormatInt(nanos[0], 10) + "-0"
	if body.Events[len(body.Events)-1].ID != wantLast {
		t.Errorf("events[last].id = %q, want %q", body.Events[len(body.Events)-1].ID, wantLast)
	}
	if body.HasMore {
		t.Errorf("has_more = true, want false (30 < limit 100)")
	}
	// Shape sanity: payload is a tool_call envelope with a command field.
	var payload map[string]any
	if err := json.Unmarshal(body.Events[0].Payload, &payload); err != nil {
		t.Fatalf("payload decode: %v", err)
	}
	if payload["tool"] != "Bash" {
		t.Errorf("payload.tool = %v, want Bash", payload["tool"])
	}
	if _, ok := payload["input"].(string); !ok {
		t.Errorf("payload.input missing or wrong type")
	}
}

func TestFeedHistory_BeforeOlderThanAllReturnsEmpty(t *testing.T) {
	dir := t.TempDir()
	const uuid = "bbbbbbbb-0000-0000-0000-000000000002"
	base := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	_ = writeJSONLFixture(t, dir, uuid, 10, base)

	h := FeedHistory(dir, historyResolver{uuid: uuid, name: "beta"})

	// Cursor older than any fixture timestamp → nothing to return.
	old := base.Add(-1 * time.Hour).UnixNano()
	cursor := strconv.FormatInt(old, 10) + "-0"
	rec := httptest.NewRecorder()
	req := newHistoryRequest("beta", "before="+cursor)
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body feedHistoryResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Events) != 0 {
		t.Errorf("events = %d, want 0", len(body.Events))
	}
	if body.HasMore {
		t.Errorf("has_more = true, want false")
	}
}

func TestFeedHistory_LimitAppliedAndHasMoreTrue(t *testing.T) {
	dir := t.TempDir()
	const uuid = "cccccccc-0000-0000-0000-000000000003"
	base := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	nanos := writeJSONLFixture(t, dir, uuid, 50, base)

	h := FeedHistory(dir, historyResolver{uuid: uuid, name: "gamma"})

	// before = id of the newest event so EVERYTHING older is in play;
	// limit=10 forces the scan to stop early.
	cursor := strconv.FormatInt(nanos[49], 10) + "-0"
	rec := httptest.NewRecorder()
	req := newHistoryRequest("gamma", "before="+cursor+"&limit=10")
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body feedHistoryResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Events) != 10 {
		t.Fatalf("events = %d, want 10", len(body.Events))
	}
	if !body.HasMore {
		t.Errorf("has_more = false, want true (39 older rows remain)")
	}
	// Events newest-first: first == event 48 (one below the cursor).
	wantFirst := strconv.FormatInt(nanos[48], 10) + "-0"
	if body.Events[0].ID != wantFirst {
		t.Errorf("events[0].id = %q, want %q", body.Events[0].ID, wantFirst)
	}
}

func TestFeedHistory_MissingBefore400(t *testing.T) {
	dir := t.TempDir()
	const uuid = "dddddddd-0000-0000-0000-000000000004"
	writeJSONLFixture(t, dir, uuid, 3, time.Now())

	h := FeedHistory(dir, historyResolver{uuid: uuid, name: "delta"})
	rec := httptest.NewRecorder()
	req := newHistoryRequest("delta", "")
	h(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "before") {
		t.Errorf("body = %q, want mention of before cursor", rec.Body.String())
	}
}

func TestFeedHistory_UnknownSession404(t *testing.T) {
	dir := t.TempDir()
	// No fixture file → resolver never matches.
	h := FeedHistory(dir, historyResolver{uuid: "x", name: "exists"})
	rec := httptest.NewRecorder()
	req := newHistoryRequest("ghost", "before=1-0")
	h(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestFeedHistory_NonGET405(t *testing.T) {
	dir := t.TempDir()
	h := FeedHistory(dir, historyResolver{uuid: "x", name: "exists"})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/exists/feed/history?before=1-0", nil)
	req.SetPathValue("name", "exists")
	h(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Allow"), "GET") {
		t.Errorf("Allow = %q, want GET", rec.Header().Get("Allow"))
	}
}

// TestFeedHistory_SpansReverseChunkBoundary ensures the reverse reader
// correctly stitches lines that straddle a 64 KB chunk boundary. We do
// this by padding each line's command field so the total file is well
// over reverseChunkSize.
func TestFeedHistory_SpansReverseChunkBoundary(t *testing.T) {
	dir := t.TempDir()
	const uuid = "eeeeeeee-0000-0000-0000-000000000005"
	path := filepath.Join(dir, uuid+".jsonl")
	fh, err := os.Create(path)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	base := time.Date(2026, 4, 21, 12, 0, 0, 0, time.UTC)
	pad := strings.Repeat("x", 2000)
	nanos := make([]int64, 0, 100)
	for i := 0; i < 100; i++ {
		ts := base.Add(time.Duration(i) * time.Second)
		tsStr := ts.UTC().Format(time.RFC3339)
		parsed, _ := time.Parse(time.RFC3339, tsStr)
		nanos = append(nanos, parsed.UnixNano())
		line := map[string]any{
			"tool_name":     "Bash",
			"tool_input":    map[string]any{"command": pad + "-" + strconv.Itoa(i)},
			"ctm_timestamp": tsStr,
		}
		enc, _ := json.Marshal(line)
		fh.Write(append(enc, '\n'))
	}
	fh.Close()

	h := FeedHistory(dir, historyResolver{uuid: uuid, name: "epsilon"})
	cursor := strconv.FormatInt(nanos[99], 10) + "-0"
	rec := httptest.NewRecorder()
	req := newHistoryRequest("epsilon", "before="+cursor+"&limit=500")
	h(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body feedHistoryResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// Expect events 0..98 = 99 rows.
	if len(body.Events) != 99 {
		t.Fatalf("events = %d, want 99 (chunk-boundary stitching broken?)", len(body.Events))
	}
	wantFirst := strconv.FormatInt(nanos[98], 10) + "-0"
	if body.Events[0].ID != wantFirst {
		t.Errorf("events[0].id = %q, want %q", body.Events[0].ID, wantFirst)
	}
	wantLast := strconv.FormatInt(nanos[0], 10) + "-0"
	if body.Events[len(body.Events)-1].ID != wantLast {
		t.Errorf("events[last].id = %q, want %q", body.Events[len(body.Events)-1].ID, wantLast)
	}
}
