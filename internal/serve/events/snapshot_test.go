package events

import (
	"testing"
)

func TestSnapshot_EmptyHubReturnsNil(t *testing.T) {
	t.Parallel()
	h := NewHub(0)
	if got := h.Snapshot(""); got != nil {
		t.Errorf("Snapshot on empty hub = %+v, want nil", got)
	}
	if got := h.Snapshot("alpha"); got != nil {
		t.Errorf("Snapshot for unknown filter = %+v, want nil", got)
	}
}

func TestSnapshot_GlobalReturnsChronologicalCopy(t *testing.T) {
	t.Parallel()
	h := NewHub(0)
	for _, payload := range [][]byte{[]byte(`{"n":1}`), []byte(`{"n":2}`), []byte(`{"n":3}`)} {
		h.Publish(Event{Type: "tool_call", Session: "alpha", Payload: payload})
	}

	got := h.Snapshot("")
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for i, want := range [][]byte{[]byte(`{"n":1}`), []byte(`{"n":2}`), []byte(`{"n":3}`)} {
		if string(got[i].Payload) != string(want) {
			t.Errorf("got[%d].Payload = %q, want %q", i, got[i].Payload, want)
		}
	}

	// Mutating the returned slice must not affect future Snapshot calls.
	got[0].Type = "mutated"
	again := h.Snapshot("")
	if again[0].Type == "mutated" {
		t.Errorf("Snapshot returned aliased slice; mutation leaked back")
	}
}

func TestSnapshot_FilterReturnsOnlySessionEvents(t *testing.T) {
	t.Parallel()
	h := NewHub(0)
	h.Publish(Event{Type: "tool_call", Session: "alpha", Payload: []byte(`{"a":1}`)})
	h.Publish(Event{Type: "tool_call", Session: "beta", Payload: []byte(`{"b":1}`)})
	h.Publish(Event{Type: "tool_call", Session: "alpha", Payload: []byte(`{"a":2}`)})

	alpha := h.Snapshot("alpha")
	if len(alpha) != 2 {
		t.Fatalf("alpha snapshot len = %d, want 2", len(alpha))
	}
	for i, ev := range alpha {
		if ev.Session != "alpha" {
			t.Errorf("alpha[%d].Session = %q, want alpha", i, ev.Session)
		}
	}

	beta := h.Snapshot("beta")
	if len(beta) != 1 || beta[0].Session != "beta" {
		t.Errorf("beta snapshot = %+v, want one beta event", beta)
	}

	global := h.Snapshot("")
	if len(global) != 3 {
		t.Errorf("global snapshot len = %d, want 3", len(global))
	}
}

func TestSnapshot_RingWrapKeepsNewest(t *testing.T) {
	t.Parallel()
	const ringSize = 4
	h := NewHub(ringSize)

	// Publish 6 events on the same session — ring should retain the last 4.
	for i := 0; i < 6; i++ {
		h.Publish(Event{
			Type:    "tool_call",
			Session: "alpha",
			Payload: []byte(`{"n":` + string(rune('0'+i)) + `}`),
		})
	}

	got := h.Snapshot("alpha")
	if len(got) != ringSize {
		t.Fatalf("len = %d, want %d", len(got), ringSize)
	}
	// Oldest retained should correspond to n=2 (events 0,1 fell off).
	wantFirst := `{"n":2}`
	if string(got[0].Payload) != wantFirst {
		t.Errorf("got[0].Payload = %q, want %q", got[0].Payload, wantFirst)
	}
	wantLast := `{"n":5}`
	if string(got[ringSize-1].Payload) != wantLast {
		t.Errorf("got[last].Payload = %q, want %q", got[ringSize-1].Payload, wantLast)
	}
}
