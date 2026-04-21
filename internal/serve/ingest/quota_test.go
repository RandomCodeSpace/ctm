package ingest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/events"
	"github.com/RandomCodeSpace/ctm/internal/session"
)

const quotaTestUUID = "ffffffff-1111-2222-3333-444444444444"

// newFakeProjection builds a Projection populated with a static slice
// without spinning up its polling Run goroutine. We're in the same
// package, so we can poke the unexported fields directly.
func newFakeProjection(sess ...session.Session) *Projection {
	p := New("/dev/null", nil)
	p.mu.Lock()
	p.sessions = append([]session.Session{}, sess...)
	for _, s := range sess {
		p.byName[s.Name] = s
	}
	p.mu.Unlock()
	return p
}

func writePayload(t *testing.T, path string, body map[string]any) {
	t.Helper()
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestQuotaIngester_GlobalEventOnInitialSweep(t *testing.T) {
	dir := t.TempDir()
	hub := events.NewHub(0)
	sub, _ := hub.Subscribe("", "")
	defer sub.Close()

	pct1 := 34.0
	pct2 := 21.0
	writePayload(t, filepath.Join(dir, quotaTestUUID+".json"), map[string]any{
		"session_id": quotaTestUUID,
		"rate_limits": map[string]any{
			"seven_day": map[string]any{"used_percentage": pct1},
			"five_hour": map[string]any{"used_percentage": pct2},
		},
	})

	q := NewQuotaIngester(dir, nil, hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = q.Run(ctx) }()

	select {
	case ev := <-sub.Events():
		if ev.Type != "quota_update" {
			t.Errorf("ev.Type = %q, want quota_update", ev.Type)
		}
		var body map[string]any
		_ = json.Unmarshal(ev.Payload, &body)
		if body["weekly_pct"] != float64(34) {
			t.Errorf("weekly_pct = %v, want 34", body["weekly_pct"])
		}
		if body["five_hr_pct"] != float64(21) {
			t.Errorf("five_hr_pct = %v, want 21", body["five_hr_pct"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no quota_update event from initial sweep")
	}
}

func TestQuotaIngester_PerSessionContextEvent(t *testing.T) {
	dir := t.TempDir()
	hub := events.NewHub(0)

	proj := newFakeProjection(session.Session{
		Name: "alpha",
		UUID: quotaTestUUID,
	})

	subAlpha, _ := hub.Subscribe("alpha", "")
	defer subAlpha.Close()

	q := NewQuotaIngester(dir, proj, hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = q.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)

	ctxPct := 49.4
	writePayload(t, filepath.Join(dir, quotaTestUUID+".json"), map[string]any{
		"session_id": quotaTestUUID,
		"context_window": map[string]any{
			"used_percentage": ctxPct,
		},
	})

	select {
	case ev := <-subAlpha.Events():
		if ev.Type != "quota_update" || ev.Session != "alpha" {
			t.Errorf("ev = %+v, want type=quota_update session=alpha", ev)
		}
		var body map[string]any
		_ = json.Unmarshal(ev.Payload, &body)
		if body["context_pct"] != float64(49) {
			t.Errorf("context_pct = %v, want 49 (rounded)", body["context_pct"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no per-session quota event")
	}

	if got, ok := q.ContextPct("alpha"); !ok || got != 49 {
		t.Errorf("ContextPct(alpha) = %d,%v want 49,true", got, ok)
	}
}

func TestQuotaIngester_NoChangeNoRepublish(t *testing.T) {
	dir := t.TempDir()
	hub := events.NewHub(0)
	sub, _ := hub.Subscribe("", "")
	defer sub.Close()

	pct := 12.0
	path := filepath.Join(dir, quotaTestUUID+".json")
	writePayload(t, path, map[string]any{
		"rate_limits": map[string]any{
			"seven_day": map[string]any{"used_percentage": pct},
			"five_hour": map[string]any{"used_percentage": pct},
		},
	})

	q := NewQuotaIngester(dir, nil, hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = q.Run(ctx) }()

	// Drain the initial event.
	select {
	case <-sub.Events():
	case <-time.After(2 * time.Second):
		t.Fatal("no initial event")
	}

	// Re-write identical contents — should NOT re-publish.
	writePayload(t, path, map[string]any{
		"rate_limits": map[string]any{
			"seven_day": map[string]any{"used_percentage": pct},
			"five_hour": map[string]any{"used_percentage": pct},
		},
	})

	select {
	case ev := <-sub.Events():
		t.Errorf("unexpected republish: %+v", ev)
	case <-time.After(300 * time.Millisecond):
		// good — no event
	}
}

func TestQuotaIngester_AccessorsReturnFalseUntilSeen(t *testing.T) {
	q := NewQuotaIngester(t.TempDir(), nil, events.NewHub(0))
	if _, ok := q.WeeklyPct(); ok {
		t.Error("WeeklyPct should be false before first ingest")
	}
	if _, ok := q.FiveHourPct(); ok {
		t.Error("FiveHourPct should be false before first ingest")
	}
	if _, ok := q.ContextPct("x"); ok {
		t.Error("ContextPct(x) should be false before first ingest")
	}
}

