package attention

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/events"
)

// TestLastToolCallAt_RecordedFromToolCall verifies the per-session
// lastCall timestamp is exposed via the public LastToolCallAt accessor.
func TestLastToolCallAt_RecordedFromToolCall(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	hub := events.NewHub(50)
	q := newFakeQuota()
	s := newFakeSessions("alpha")
	eng := newEngineAt(&now, hub, q, s, Defaults())

	// Unknown session → ok=false.
	if _, ok := eng.LastToolCallAt("ghost"); ok {
		t.Fatalf("LastToolCallAt(ghost) = ok, want !ok")
	}

	// Tracked session with no calls yet → ok=false.
	eng.handleEvent(events.Event{
		Type:    "tool_call",
		Session: "alpha",
		Payload: mustPayload(t, map[string]any{
			"session":  "alpha",
			"is_error": false,
			"ts":       now,
		}),
	})

	got, ok := eng.LastToolCallAt("alpha")
	if !ok {
		t.Fatalf("LastToolCallAt after tool_call = !ok, want ok")
	}
	if !got.Equal(now) {
		t.Errorf("LastToolCallAt = %v, want %v", got, now)
	}

	// A second tool_call with a later ts replaces the recorded value.
	later := now.Add(2 * time.Minute)
	eng.handleEvent(events.Event{
		Type:    "tool_call",
		Session: "alpha",
		Payload: mustPayload(t, map[string]any{
			"session":  "alpha",
			"is_error": false,
			"ts":       later,
		}),
	})
	got2, _ := eng.LastToolCallAt("alpha")
	if !got2.Equal(later) {
		t.Errorf("after second call: %v, want %v", got2, later)
	}
}

// TestSessionNameFromPayload_Fallbacks covers the three branches of
// sessionNameFromPayload: explicit ev.Session, payload "name", and the
// final return path. We exercise it through handleEvent so the test
// only relies on the public surface.
func TestHandleEvent_OnYolo_NameFromPayload(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	hub := events.NewHub(50)
	q := newFakeQuota()
	s := newFakeSessions() // no preregistered names
	thr := Defaults()
	thr.YoloUncheckedMinutes = 30
	eng := newEngineAt(&now, hub, q, s, thr)

	// Event has empty ev.Session — name must come from payload.name.
	payload, _ := json.Marshal(map[string]any{"name": "alpha"})
	eng.handleEvent(events.Event{Type: "on_yolo", Payload: payload})

	// markYolo only sets state; G must NOT trip on a fresh entry.
	// But the yoloAt timestamp is now == clock; advancing the clock
	// past the threshold without a checkpoint and re-evaluating
	// triggers G.
	s.names = append(s.names, "alpha")
	s.alive["alpha"] = true
	s.modes["alpha"] = "yolo"

	// 31 minutes later → trip.
	now = now.Add(31 * time.Minute)
	eng.evaluateAll()
	snap, ok := eng.Snapshot("alpha")
	if !ok || snap.State != StateYoloUnchecked {
		t.Fatalf("want yolo_unchecked after threshold elapsed, got %+v ok=%v", snap, ok)
	}
}

// TestHandleEvent_OnYolo_EmptyNamesIgnored ensures markYolo is not
// called when the event has neither ev.Session nor a payload name.
func TestHandleEvent_OnYolo_NoNameIgnored(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	hub := events.NewHub(50)
	eng := newEngineAt(&now, hub, newFakeQuota(), newFakeSessions(), Defaults())

	eng.handleEvent(events.Event{Type: "on_yolo", Payload: []byte(`{}`)})
	// No session ever added — Snapshot of unknown name is ok=false.
	if _, ok := eng.Snapshot(""); ok {
		t.Fatalf("expected no state created for nameless event")
	}
}

// TestHandleEvent_OnYolo_BadJSONIgnored makes sure malformed payloads
// don't panic or create stray state.
func TestHandleEvent_OnYolo_BadJSONIgnored(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	hub := events.NewHub(50)
	eng := newEngineAt(&now, hub, newFakeQuota(), newFakeSessions(), Defaults())

	eng.handleEvent(events.Event{Type: "on_yolo", Payload: []byte(`{not json`)})
	if _, ok := eng.Snapshot("anything"); ok {
		t.Fatalf("expected no state created for bad payload")
	}
}

// TestHandleEvent_SessionKilled_EvSessionWins exercises markTmuxDead
// via the explicit ev.Session path. The dead state shows up only on
// next evaluateAll because markTmuxDead is a no-op for state and
// SessionSource.TmuxAlive is the source of truth.
func TestHandleEvent_SessionKilled_TriggersDead(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	hub := events.NewHub(50)
	s := newFakeSessions("alpha")
	eng := newEngineAt(&now, hub, newFakeQuota(), s, Defaults())

	// Mark the session dead in the SessionSource and dispatch the event.
	s.setAlive("alpha", false)
	eng.handleEvent(events.Event{Type: "session_killed", Session: "alpha", Payload: []byte(`{}`)})
	eng.evaluateAll()
	snap, ok := eng.Snapshot("alpha")
	if !ok || snap.State != StateTmuxDead {
		t.Fatalf("want tmux_dead, got %+v ok=%v", snap, ok)
	}
}

// TestHandleEvent_ToolCall_IgnoresUnknownSession covers the early
// return when neither ev.Session nor payload.session is set.
func TestHandleEvent_ToolCall_NoSessionIgnored(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	hub := events.NewHub(50)
	eng := newEngineAt(&now, hub, newFakeQuota(), newFakeSessions(), Defaults())

	payload, _ := json.Marshal(map[string]any{"is_error": true})
	eng.handleEvent(events.Event{Type: "tool_call", Payload: payload})

	// Nothing should have been created.
	if _, ok := eng.LastToolCallAt(""); ok {
		t.Fatalf("expected no tracked session for nameless tool_call")
	}
}

// TestHandleEvent_ToolCall_BadJSONIgnored covers the json.Unmarshal
// error branch of handleEvent.
func TestHandleEvent_ToolCall_BadJSONIgnored(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	hub := events.NewHub(50)
	eng := newEngineAt(&now, hub, newFakeQuota(), newFakeSessions("alpha"), Defaults())

	eng.handleEvent(events.Event{Type: "tool_call", Session: "alpha", Payload: []byte(`{garbage`)})
	if _, ok := eng.LastToolCallAt("alpha"); ok {
		t.Fatalf("expected no tracking from malformed payload")
	}
}

// TestHandleEvent_ToolCall_ZeroTSFallsBackToClock checks the
// "ts.IsZero() → e.now()" branch of handleEvent.
func TestHandleEvent_ToolCall_ZeroTSFallsBackToClock(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	hub := events.NewHub(50)
	eng := newEngineAt(&now, hub, newFakeQuota(), newFakeSessions("alpha"), Defaults())

	// Payload with no "ts" field → recordToolCall must use e.now().
	payload, _ := json.Marshal(map[string]any{"session": "alpha", "is_error": false})
	eng.handleEvent(events.Event{Type: "tool_call", Session: "alpha", Payload: payload})

	got, ok := eng.LastToolCallAt("alpha")
	if !ok {
		t.Fatalf("LastToolCallAt(alpha) = !ok")
	}
	if !got.Equal(now) {
		t.Errorf("LastToolCallAt = %v, want fallback to now=%v", got, now)
	}
}

// TestMarkYolo_IdempotentOnRepeatedEvents ensures that re-firing
// on_yolo for the same session does NOT reset yoloAt — only the first
// observation is recorded.
func TestMarkYolo_IdempotentOnRepeatedEvents(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	hub := events.NewHub(50)
	s := newFakeSessions("alpha")
	thr := Defaults()
	thr.YoloUncheckedMinutes = 30
	eng := newEngineAt(&now, hub, newFakeQuota(), s, thr)

	s.setMode("alpha", "yolo")

	// First event sets yoloAt at t=now.
	eng.handleEvent(events.Event{Type: "on_yolo", Session: "alpha", Payload: []byte(`{}`)})

	// Advance clock and fire on_yolo again — yoloAt must not bump forward,
	// otherwise the threshold check below would not fire.
	now = now.Add(20 * time.Minute)
	eng.handleEvent(events.Event{Type: "on_yolo", Session: "alpha", Payload: []byte(`{}`)})

	// Total elapsed = 31 min → G fires.
	now = now.Add(11 * time.Minute)
	eng.evaluateAll()
	snap, ok := eng.Snapshot("alpha")
	if !ok || snap.State != StateYoloUnchecked {
		t.Fatalf("want yolo_unchecked (idempotent yoloAt), got %+v ok=%v", snap, ok)
	}
}
