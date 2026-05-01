package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/serve/events"
)

// fakeFeedSource is a minimal FeedSource backed by a static slice so we
// can exercise Feed without standing up the real hub.
type fakeFeedSource struct{ events []events.Event }

func (f fakeFeedSource) Snapshot(filter string) []events.Event {
	if filter == "" {
		return f.events
	}
	out := make([]events.Event, 0, len(f.events))
	for _, ev := range f.events {
		if ev.Session == filter {
			out = append(out, ev)
		}
	}
	return out
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestFeed_FiltersToToolCallsOnlyAndReverses(t *testing.T) {
	src := fakeFeedSource{events: []events.Event{
		{Type: "tool_call", Session: "alpha", Payload: mustJSON(t, map[string]any{"n": 1})},
		{Type: "quota_update", Session: "", Payload: mustJSON(t, map[string]any{"n": 2})},
		{Type: "tool_call", Session: "alpha", Payload: mustJSON(t, map[string]any{"n": 3})},
		{Type: "attention_raised", Session: "alpha", Payload: mustJSON(t, map[string]any{"n": 4})},
		{Type: "tool_call", Session: "alpha", Payload: mustJSON(t, map[string]any{"n": 5})},
	}}
	h := Feed(src, "")
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/feed", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}

	var got []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d items, want 3 tool_calls only: %+v", len(got), got)
	}
	// Newest-first: original chronological order [1,3,5] reverses to [5,3,1].
	wantNs := []float64{5, 3, 1}
	for i, want := range wantNs {
		if got[i]["n"] != want {
			t.Errorf("item %d n = %v, want %v", i, got[i]["n"], want)
		}
	}
}

func TestFeed_EmptyRingReturnsEmptyArray(t *testing.T) {
	h := Feed(fakeFeedSource{}, "")
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/feed", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	// Body must be a JSON array literal "[]" (not "null") so the SPA
	// can distinguish empty-but-known from "fetch failed".
	body := rec.Body.String()
	if body != "[]\n" && body != "[]" {
		t.Errorf("body = %q, want %q", body, "[]")
	}
}

func TestFeed_LimitClampedToMax(t *testing.T) {
	// Build 600 tool_calls; expect at most maxFeedLimit (500) returned.
	in := make([]events.Event, 0, 600)
	for i := 0; i < 600; i++ {
		in = append(in, events.Event{
			Type:    "tool_call",
			Session: "alpha",
			Payload: mustJSON(t, map[string]any{"n": i}),
		})
	}
	h := Feed(fakeFeedSource{events: in}, "")
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/feed?limit=99999", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != maxFeedLimit {
		t.Errorf("len(got) = %d, want %d (clamped)", len(got), maxFeedLimit)
	}
}

func TestFeed_LimitHonouredWhenSmall(t *testing.T) {
	in := make([]events.Event, 0, 10)
	for i := 0; i < 10; i++ {
		in = append(in, events.Event{
			Type:    "tool_call",
			Session: "alpha",
			Payload: mustJSON(t, map[string]any{"n": i}),
		})
	}
	h := Feed(fakeFeedSource{events: in}, "")
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/feed?limit=3", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("len(got) = %d, want 3", len(got))
	}
	// Newest-first.
	if got[0]["n"] != float64(9) {
		t.Errorf("first item n = %v, want 9", got[0]["n"])
	}
}

func TestFeed_InvalidLimitFallsBackToDefault(t *testing.T) {
	// 250 tool_calls; ?limit=garbage → defaultFeedLimit (200).
	in := make([]events.Event, 0, 250)
	for i := 0; i < 250; i++ {
		in = append(in, events.Event{
			Type:    "tool_call",
			Session: "alpha",
			Payload: mustJSON(t, map[string]any{"n": i}),
		})
	}
	h := Feed(fakeFeedSource{events: in}, "")
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/feed?limit=banana", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != defaultFeedLimit {
		t.Errorf("len(got) = %d, want default %d", len(got), defaultFeedLimit)
	}
}

func TestFeed_PerSessionFilterFromConstructor(t *testing.T) {
	src := fakeFeedSource{events: []events.Event{
		{Type: "tool_call", Session: "alpha", Payload: mustJSON(t, map[string]any{"who": "alpha"})},
		{Type: "tool_call", Session: "beta", Payload: mustJSON(t, map[string]any{"who": "beta"})},
	}}
	h := Feed(src, "alpha")
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/api/sessions/alpha/feed", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0]["who"] != "alpha" {
		t.Errorf("got %+v, want only alpha", got)
	}
}

func TestFeed_MethodNotAllowed(t *testing.T) {
	h := Feed(fakeFeedSource{}, "")
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/api/feed", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
	if got := rec.Header().Get("Allow"); got != http.MethodGet {
		t.Errorf("Allow = %q, want GET", got)
	}
}
