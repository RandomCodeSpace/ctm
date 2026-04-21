package events

import (
	"encoding/json"
	"sync"
	"testing"
	"time"
)

func mustPayload(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func TestHub_BasicFanout(t *testing.T) {
	t.Parallel()
	h := NewHub(0)
	subs := make([]*Sub, 3)
	for i := range subs {
		s, _ := h.Subscribe("", "")
		subs[i] = s
	}
	defer func() {
		for _, s := range subs {
			s.Close()
		}
	}()

	h.Publish(Event{Type: "tool_call", Payload: mustPayload(t, map[string]string{"k": "v"})})

	for i, s := range subs {
		select {
		case e := <-s.Events():
			if e.Type != "tool_call" {
				t.Fatalf("sub %d: got type %q", i, e.Type)
			}
			if e.ID == "" {
				t.Fatalf("sub %d: id not assigned", i)
			}
		case <-time.After(time.Second):
			t.Fatalf("sub %d: timed out waiting for event", i)
		}
	}
}

func TestHub_FilterBySession(t *testing.T) {
	t.Parallel()
	h := NewHub(0)
	all, _ := h.Subscribe("", "")
	defer all.Close()
	alpha, _ := h.Subscribe("alpha", "")
	defer alpha.Close()

	h.Publish(Event{Type: "tool_call", Session: "alpha", Payload: []byte(`{}`)})
	h.Publish(Event{Type: "tool_call", Session: "beta", Payload: []byte(`{}`)})

	// alpha-filtered sub should see only the alpha event.
	select {
	case e := <-alpha.Events():
		if e.Session != "alpha" {
			t.Fatalf("alpha sub got session %q", e.Session)
		}
	case <-time.After(time.Second):
		t.Fatal("alpha sub timed out")
	}
	select {
	case e := <-alpha.Events():
		t.Fatalf("alpha sub got unexpected event: %+v", e)
	case <-time.After(50 * time.Millisecond):
	}

	// global sub should see both.
	for i := 0; i < 2; i++ {
		select {
		case <-all.Events():
		case <-time.After(time.Second):
			t.Fatalf("global sub timed out on event %d", i)
		}
	}
}

func TestHub_RingReplay(t *testing.T) {
	t.Parallel()
	h := NewHub(50)
	const n = 20

	// Publish n events with no subscribers; they accumulate in the ring.
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		e := Event{Type: "tool_call", Payload: []byte(`{}`)}
		h.Publish(e)
		// Read back the assigned ID via the global ring snapshot.
		ids[i] = h.rings[globalRing].snapshot()[i].ID
	}

	// Subscribe with since = ids[k]; expect n-1-k events replayed.
	const k = 7
	sub, replay := h.Subscribe("", ids[k])
	defer sub.Close()

	want := n - 1 - k
	if len(replay) != want {
		t.Fatalf("replay len = %d, want %d", len(replay), want)
	}
	for i, e := range replay {
		if e.ID != ids[k+1+i] {
			t.Fatalf("replay[%d] id = %q, want %q", i, e.ID, ids[k+1+i])
		}
	}
}

func TestHub_RingReplayGapMarker(t *testing.T) {
	t.Parallel()
	// Tiny ring: 5 entries. Publish 10. Subscribe with a very old since.
	h := NewHub(5)
	for i := 0; i < 10; i++ {
		h.Publish(Event{Type: "x", Payload: []byte(`{}`)})
	}
	// since = "0-0" predates everything; replay should be the full ring.
	sub, replay := h.Subscribe("", "0-0")
	defer sub.Close()
	if len(replay) != 5 {
		t.Fatalf("expected full ring (5) on unfillable gap, got %d", len(replay))
	}
}

func TestHub_SlowConsumerDrop(t *testing.T) {
	t.Parallel()
	h := NewHub(0)
	slow, _ := h.Subscribe("", "")
	defer slow.Close()

	// Anchor to the buffer size so this keeps testing "overflow"
	// behaviour if subChanBuffer changes in the future.
	n := subChanBuffer + 500
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < n; i++ {
			h.Publish(Event{Type: "x", Payload: []byte(`{}`)})
		}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatalf("publisher blocked on slow consumer")
	}

	// Drain what slow can hold (subChanBuffer) and verify the rest were dropped.
	delivered := 0
drain:
	for {
		select {
		case <-slow.Events():
			delivered++
		default:
			break drain
		}
	}
	if delivered > subChanBuffer {
		t.Fatalf("delivered %d > channel buffer %d", delivered, subChanBuffer)
	}
	if slow.Dropped() == 0 {
		t.Fatal("expected non-zero dropped counter")
	}
	if uint64(delivered)+slow.Dropped() != uint64(n) {
		t.Fatalf("delivered(%d) + dropped(%d) != %d", delivered, slow.Dropped(), n)
	}
	t.Logf("slow consumer: published=%d delivered=%d dropped=%d", n, delivered, slow.Dropped())
}

func TestSub_CloseIdempotent(t *testing.T) {
	t.Parallel()
	h := NewHub(0)
	s, _ := h.Subscribe("", "")
	s.Close()
	// second close must not panic or double-close the channel.
	s.Close()
	// reading from a closed channel must return zero+!ok.
	if _, ok := <-s.Events(); ok {
		t.Fatal("expected channel to be closed")
	}
}

func TestHub_IDMonotonicAndUnique(t *testing.T) {
	t.Parallel()
	h := NewHub(2000)
	const n = 1000
	for i := 0; i < n; i++ {
		h.Publish(Event{Type: "x", Payload: []byte(`{}`)})
	}
	snap := h.rings[globalRing].snapshot()
	if len(snap) != n {
		t.Fatalf("ring size %d, want %d", len(snap), n)
	}
	seen := make(map[string]struct{}, n)
	for i, e := range snap {
		if _, dup := seen[e.ID]; dup {
			t.Fatalf("duplicate id at %d: %q", i, e.ID)
		}
		seen[e.ID] = struct{}{}
		if i > 0 && !idLessThan(snap[i-1].ID, e.ID) {
			t.Fatalf("id non-monotonic at %d: prev=%q cur=%q",
				i, snap[i-1].ID, e.ID)
		}
	}
}

func TestHub_StatsAccessor(t *testing.T) {
	t.Parallel()
	h := NewHub(10)
	a, _ := h.Subscribe("", "")
	defer a.Close()
	b, _ := h.Subscribe("alpha", "")
	defer b.Close()

	h.Publish(Event{Type: "x", Session: "alpha", Payload: []byte(`{}`)})
	h.Publish(Event{Type: "x", Payload: []byte(`{}`)})
	// Drain so subs can keep up (avoids racy drop counters).
	<-a.Events()
	<-a.Events()
	<-b.Events()

	st := h.Stats()
	if st.Published != 2 {
		t.Fatalf("published = %d, want 2", st.Published)
	}
	if st.Subscribers != 2 {
		t.Fatalf("subscribers = %d, want 2", st.Subscribers)
	}
	if st.RingSizes[globalRing] != 2 {
		t.Fatalf("global ring = %d, want 2", st.RingSizes[globalRing])
	}
	if st.RingSizes["alpha"] != 1 {
		t.Fatalf("alpha ring = %d, want 1", st.RingSizes["alpha"])
	}
}

func TestHub_ConcurrentPublishSubscribe(t *testing.T) {
	t.Parallel()
	h := NewHub(0)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				h.Publish(Event{Type: "x", Payload: []byte(`{}`)})
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, _ := h.Subscribe("", "")
			defer s.Close()
			deadline := time.After(500 * time.Millisecond)
			for {
				select {
				case <-s.Events():
				case <-deadline:
					return
				}
			}
		}()
	}
	wg.Wait()
}
