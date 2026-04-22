package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeSearchSource is an in-memory SearchSource for tests.
type fakeSearchSource struct {
	hits      []SearchHit
	truncated bool
	err       error
	// Record the last call arguments so tests can assert plumbing.
	lastQ, lastSess string
	lastLimit       int
}

func (f *fakeSearchSource) SearchFTS(q, sess string, limit int) ([]SearchHit, bool, error) {
	f.lastQ, f.lastSess, f.lastLimit = q, sess, limit
	if f.err != nil {
		return nil, false, f.err
	}
	// Respect the limit so the handler's truncated flag stays
	// driven by the store, not the handler.
	n := len(f.hits)
	if n > limit {
		n = limit
	}
	return f.hits[:n], f.truncated, nil
}

// fakeSessionResolver implements SessionNameResolver for tests.
type fakeSessionResolver struct{ m map[string]string }

func (f fakeSessionResolver) ResolveSessionName(name string) (string, bool) {
	u, ok := f.m[name]
	return u, ok && u != ""
}

func TestSearch_ReturnsIndexHits(t *testing.T) {
	ts := time.Date(2026, 4, 21, 16, 28, 0, 0, time.UTC)
	src := &fakeSearchSource{
		hits: []SearchHit{
			{Session: "alpha", TS: ts, Tool: "Bash", Snippet: "echo hello-needle-world"},
			{Session: "beta", TS: ts.Add(time.Minute), Tool: "Grep", Snippet: "pattern=needle"},
		},
	}
	resolver := fakeSessionResolver{m: map[string]string{
		"alpha": "11111111-0000-0000-0000-000000000001",
		"beta":  "22222222-0000-0000-0000-000000000002",
	}}

	h := Search(src, resolver)
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
	if len(resp.Matches) != 2 {
		t.Fatalf("matches=%d want 2", len(resp.Matches))
	}
	if resp.Matches[0].UUID == "" || resp.Matches[1].UUID == "" {
		t.Errorf("uuid not resolved: %+v", resp.Matches)
	}
	for _, m := range resp.Matches {
		if !strings.Contains(strings.ToLower(m.Snippet), "needle") {
			t.Errorf("snippet missing query: %q", m.Snippet)
		}
		if m.TS == "" {
			t.Errorf("ts empty: %+v", m)
		}
	}
	if src.lastQ != "needle" {
		t.Errorf("store saw q=%q want needle", src.lastQ)
	}
}

func TestSearch_PassesSessionFilter(t *testing.T) {
	src := &fakeSearchSource{
		hits: []SearchHit{
			{Session: "beta", TS: time.Now().UTC(), Tool: "Bash", Snippet: "hit"},
		},
	}
	h := Search(src, fakeSessionResolver{})
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=nee&session=beta", nil)
	rr := httptest.NewRecorder()
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	if src.lastSess != "beta" {
		t.Errorf("store saw session=%q want beta", src.lastSess)
	}
}

func TestSearch_PropagatesTruncated(t *testing.T) {
	src := &fakeSearchSource{
		hits: []SearchHit{
			{Session: "a", TS: time.Now().UTC(), Tool: "Bash", Snippet: "one"},
			{Session: "a", TS: time.Now().UTC(), Tool: "Bash", Snippet: "two"},
		},
		truncated: true,
	}
	h := Search(src, fakeSessionResolver{})
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=nee&limit=1", nil)
	rr := httptest.NewRecorder()
	h(rr, req)

	var resp SearchResponse
	_ = json.Unmarshal(rr.Body.Bytes(), &resp)
	if !resp.Truncated {
		t.Errorf("truncated=false want true")
	}
	if len(resp.Matches) != 1 {
		t.Errorf("matches=%d want 1 (limit)", len(resp.Matches))
	}
	if src.lastLimit != 1 {
		t.Errorf("store saw limit=%d want 1", src.lastLimit)
	}
}

func TestSearch_BadQuery400(t *testing.T) {
	h := Search(&fakeSearchSource{}, fakeSessionResolver{})
	for _, q := range []string{"", "x", "ab"} {
		req := httptest.NewRequest(http.MethodGet, "/api/search?q="+q, nil)
		rr := httptest.NewRecorder()
		h(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("q=%q status=%d want 400", q, rr.Code)
		}
	}
}

func TestSearch_LimitClampedToMax(t *testing.T) {
	src := &fakeSearchSource{}
	h := Search(src, fakeSessionResolver{})
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=nee&limit=100000", nil)
	rr := httptest.NewRecorder()
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d", rr.Code)
	}
	if src.lastLimit != searchMaxLimit {
		t.Errorf("limit=%d want %d", src.lastLimit, searchMaxLimit)
	}
}

func TestSearch_500OnStoreError(t *testing.T) {
	src := &fakeSearchSource{err: errors.New("fts locked")}
	h := Search(src, fakeSessionResolver{})
	req := httptest.NewRequest(http.MethodGet, "/api/search?q=nee", nil)
	rr := httptest.NewRecorder()
	h(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status=%d want 500", rr.Code)
	}
}

func TestSearch_405OnPost(t *testing.T) {
	h := Search(&fakeSearchSource{}, fakeSessionResolver{})
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
