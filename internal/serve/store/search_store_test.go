package store

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/events"
)

func newTestStore(t *testing.T) *sqliteCostStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ctm.db")
	s, err := OpenCostStore(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s.(*sqliteCostStore)
}

func TestSearchFTS_FindsLiteralSubstring(t *testing.T) {
	s := newTestStore(t)
	ts := time.Date(2026, 4, 21, 16, 28, 0, 0, time.UTC)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(s.IndexToolCall("alpha", "Bash", "echo has-needle-row here", ts))
	must(s.IndexToolCall("alpha", "Read", "/tmp/foo.txt", ts.Add(time.Second)))
	must(s.IndexToolCall("beta", "Grep", "pattern=needle", ts.Add(2*time.Second)))

	hits, truncated, err := s.SearchFTS("needle", "", 100)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if truncated {
		t.Errorf("unexpected truncated=true")
	}
	if len(hits) != 2 {
		t.Fatalf("hits=%d want 2 (got %+v)", len(hits), hits)
	}
	for _, h := range hits {
		if !strings.Contains(strings.ToLower(h.Snippet), "needle") {
			t.Errorf("snippet missing query: %q", h.Snippet)
		}
	}
}

func TestSearchFTS_SessionFilter(t *testing.T) {
	s := newTestStore(t)
	ts := time.Now().UTC()

	_ = s.IndexToolCall("alpha", "Bash", "needle-in-alpha", ts)
	_ = s.IndexToolCall("beta", "Bash", "needle-in-beta", ts)

	hits, _, err := s.SearchFTS("needle", "beta", 100)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits=%d want 1", len(hits))
	}
	if hits[0].Session != "beta" {
		t.Errorf("session=%q want beta", hits[0].Session)
	}
}

func TestSearchFTS_Truncation(t *testing.T) {
	s := newTestStore(t)
	base := time.Now().UTC()
	for i := 0; i < 5; i++ {
		_ = s.IndexToolCall("alpha", "Bash", "row-needle-"+string(rune('a'+i)), base.Add(time.Duration(i)*time.Second))
	}

	hits, truncated, err := s.SearchFTS("needle", "", 3)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !truncated {
		t.Errorf("truncated=false want true")
	}
	if len(hits) != 3 {
		t.Errorf("hits=%d want 3", len(hits))
	}
}

func TestSearchFTS_PathLikeTokens(t *testing.T) {
	s := newTestStore(t)
	_ = s.IndexToolCall("alpha", "Edit", "src/live.ts updated", time.Now().UTC())

	// Trigram tokenizer handles substring queries even across
	// slashes — this is the property that motivated the choice.
	hits, _, err := s.SearchFTS("live.ts", "", 10)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 1 {
		t.Fatalf("hits=%d want 1 (%+v)", len(hits), hits)
	}
}

func TestSearchFTS_EmptyContentSkipped(t *testing.T) {
	s := newTestStore(t)
	// Empty content → nothing indexed; whitespace-only too.
	if err := s.IndexToolCall("alpha", "Bash", "", time.Now().UTC()); err != nil {
		t.Errorf("empty content should not error, got %v", err)
	}
	if err := s.IndexToolCall("alpha", "Bash", "   ", time.Now().UTC()); err != nil {
		t.Errorf("whitespace content should not error, got %v", err)
	}
	hits, _, _ := s.SearchFTS("alpha", "", 10)
	if len(hits) != 0 {
		t.Errorf("empty-content inserts leaked into FTS: %+v", hits)
	}
}

func TestSearchFTS_ClosedStoreErrors(t *testing.T) {
	s := newTestStore(t)
	_ = s.Close()
	if _, _, err := s.SearchFTS("needle", "", 10); err == nil {
		t.Error("expected error after Close()")
	}
	if err := s.IndexToolCall("alpha", "Bash", "x", time.Now().UTC()); err == nil {
		t.Error("expected error after Close()")
	}
}

// ---- subscriber --------------------------------------------------------

func TestSubscribeToolCallWriter_IndexesPublishedEvents(t *testing.T) {
	s := newTestStore(t)
	hub := events.NewHub(0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ready := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		SubscribeToolCallWriter(ctx, hub, s, ready)
	}()
	<-ready

	payload, _ := json.Marshal(map[string]any{
		"session":  "alpha",
		"tool":     "Bash",
		"input":    "echo haystack-needle-here",
		"summary":  "ok",
		"is_error": false,
		"ts":       time.Now().UTC().Format(time.RFC3339Nano),
	})
	hub.Publish(events.Event{Type: "tool_call", Session: "alpha", Payload: payload})

	// Poll briefly for the async insert.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		hits, _, _ := s.SearchFTS("needle", "", 10)
		if len(hits) == 1 && hits[0].Session == "alpha" {
			cancel()
			<-done
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	t.Fatal("subscriber did not index the published event")
}

func TestSubscribeToolCallWriter_IgnoresNonToolCallEvents(t *testing.T) {
	s := newTestStore(t)
	hub := events.NewHub(0)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ready := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		SubscribeToolCallWriter(ctx, hub, s, ready)
	}()
	<-ready

	hub.Publish(events.Event{
		Type:    "quota_update",
		Session: "alpha",
		Payload: []byte(`{"session":"alpha","input_tokens":1}`),
	})

	// Give the subscriber a chance to (not) process it.
	time.Sleep(100 * time.Millisecond)

	hits, _, _ := s.SearchFTS("needle", "", 10)
	if len(hits) != 0 {
		t.Errorf("quota_update leaked into FTS: %+v", hits)
	}
	cancel()
	<-done
}

func TestWipeFTSOnBoot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ctm.db")
	s1, err := OpenCostStore(path)
	if err != nil {
		t.Fatal(err)
	}
	ss1 := s1.(*sqliteCostStore)
	_ = ss1.IndexToolCall("alpha", "Bash", "needle-one", time.Now().UTC())
	hits, _, _ := ss1.SearchFTS("needle", "", 10)
	if len(hits) != 1 {
		t.Fatalf("pre-close hits=%d want 1", len(hits))
	}
	_ = s1.Close()

	// Re-open — OpenCostStore should wipe the FTS table so the
	// tailer's replay can rebuild it fresh without duplicates.
	s2, err := OpenCostStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s2.Close() }()
	ss2 := s2.(*sqliteCostStore)
	hits, _, _ = ss2.SearchFTS("needle", "", 10)
	if len(hits) != 0 {
		t.Errorf("boot wipe missed: %d rows survived restart", len(hits))
	}
}
