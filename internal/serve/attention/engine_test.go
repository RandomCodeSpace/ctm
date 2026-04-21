package attention

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/events"
)

// --- fakes ---------------------------------------------------------------

type fakeQuota struct {
	mu          sync.Mutex
	weekly      float64
	weeklyOK    bool
	fiveHr      float64
	fiveHrOK    bool
	ctx         map[string]int
}

func newFakeQuota() *fakeQuota { return &fakeQuota{ctx: make(map[string]int)} }

func (f *fakeQuota) WeeklyPct() (float64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.weekly, f.weeklyOK
}

func (f *fakeQuota) FiveHourPct() (float64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fiveHr, f.fiveHrOK
}

func (f *fakeQuota) ContextPct(name string) (int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.ctx[name]
	return v, ok
}

func (f *fakeQuota) setWeekly(p float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.weekly = p
	f.weeklyOK = true
}

func (f *fakeQuota) setFiveHr(p float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fiveHr = p
	f.fiveHrOK = true
}

func (f *fakeQuota) setCtx(name string, p int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ctx[name] = p
}

type fakeSessions struct {
	mu    sync.Mutex
	names []string
	modes map[string]string
	alive map[string]bool
	cp    map[string]time.Time
}

func newFakeSessions(names ...string) *fakeSessions {
	f := &fakeSessions{
		names: append([]string{}, names...),
		modes: make(map[string]string),
		alive: make(map[string]bool),
		cp:    make(map[string]time.Time),
	}
	for _, n := range names {
		f.alive[n] = true
	}
	return f
}

func (f *fakeSessions) Names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.names))
	copy(out, f.names)
	return out
}

func (f *fakeSessions) Mode(name string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.modes[name]
}

func (f *fakeSessions) TmuxAlive(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.alive[name]
}

func (f *fakeSessions) LastCheckpointAt(name string) (time.Time, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.cp[name]
	return t, ok
}

func (f *fakeSessions) setMode(name, mode string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.modes[name] = mode
}

func (f *fakeSessions) setAlive(name string, alive bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.alive[name] = alive
}

func (f *fakeSessions) setCheckpoint(name string, t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cp[name] = t
}

// --- helpers --------------------------------------------------------------

func mustPayload(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

func toolCallEv(t *testing.T, session string, isErr bool, ts time.Time) events.Event {
	return events.Event{
		Type:    "tool_call",
		Session: session,
		Payload: mustPayload(t, map[string]any{
			"session":  session,
			"is_error": isErr,
			"ts":       ts,
		}),
	}
}

// newEngineAt returns an engine whose clock is controlled by a pointer.
func newEngineAt(now *time.Time, hub *events.Hub, q QuotaSource, s SessionSource, thr Thresholds) *Engine {
	return NewEngine(hub, q, s, thr, func() time.Time { return *now })
}

// --- trigger tests --------------------------------------------------------

func TestTriggerA_LastErrorCall(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	hub := events.NewHub(50)
	q := newFakeQuota()
	s := newFakeSessions("alpha")
	eng := newEngineAt(&now, hub, q, s, Defaults())

	eng.handleEvent(toolCallEv(t, "alpha", true, now))
	eng.evaluateAll()

	snap, ok := eng.Snapshot("alpha")
	if !ok || snap.State != StateLastErrorCall {
		t.Fatalf("want last_error_call, got %+v ok=%v", snap, ok)
	}

	// Next non-error tool call clears.
	eng.handleEvent(toolCallEv(t, "alpha", false, now))
	eng.evaluateAll()
	if _, ok := eng.Snapshot("alpha"); ok {
		t.Fatalf("expected clear after non-error call")
	}
}

func TestTriggerB_ErrorBurst(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	hub := events.NewHub(50)
	q := newFakeQuota()
	s := newFakeSessions("alpha")
	thr := Defaults()
	thr.ErrorRateWindow = 10
	thr.ErrorRatePct = 30
	eng := newEngineAt(&now, hub, q, s, thr)

	// 10 calls, 4 errors (40%) — but ends on a non-error so A is clear.
	pattern := []bool{true, false, true, false, true, false, true, false, false, false}
	for _, isErr := range pattern {
		eng.handleEvent(toolCallEv(t, "alpha", isErr, now))
	}
	eng.evaluateAll()

	snap, ok := eng.Snapshot("alpha")
	if !ok || snap.State != StateErrorBurst {
		t.Fatalf("want error_burst, got %+v ok=%v", snap, ok)
	}

	// Drop error share by feeding 10 clean calls.
	for i := 0; i < 10; i++ {
		eng.handleEvent(toolCallEv(t, "alpha", false, now))
	}
	eng.evaluateAll()
	if _, ok := eng.Snapshot("alpha"); ok {
		t.Fatalf("expected clear after clean window")
	}
}

func TestTriggerC_Stuck(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	hub := events.NewHub(50)
	q := newFakeQuota()
	s := newFakeSessions("alpha")
	thr := Defaults()
	thr.IdleMinutes = 5
	eng := newEngineAt(&now, hub, q, s, thr)

	// Seed one call 6 minutes ago.
	eng.handleEvent(toolCallEv(t, "alpha", false, now))
	now = now.Add(6 * time.Minute)
	eng.evaluateAll()

	snap, ok := eng.Snapshot("alpha")
	if !ok || snap.State != StateStuck {
		t.Fatalf("want stuck, got %+v ok=%v", snap, ok)
	}

	// A new tool call resets the idle clock.
	eng.handleEvent(toolCallEv(t, "alpha", false, now))
	eng.evaluateAll()
	if _, ok := eng.Snapshot("alpha"); ok {
		t.Fatalf("expected clear after fresh tool_call")
	}
}

func TestTriggerD_TmuxDead(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	hub := events.NewHub(50)
	q := newFakeQuota()
	s := newFakeSessions("alpha")
	eng := newEngineAt(&now, hub, q, s, Defaults())

	s.setAlive("alpha", false)
	eng.evaluateAll()

	snap, ok := eng.Snapshot("alpha")
	if !ok || snap.State != StateTmuxDead {
		t.Fatalf("want tmux_dead, got %+v ok=%v", snap, ok)
	}

	// Revived (e.g. tmux session restarted) → clears.
	s.setAlive("alpha", true)
	eng.evaluateAll()
	if _, ok := eng.Snapshot("alpha"); ok {
		t.Fatalf("expected clear after tmux revived")
	}
}

func TestTriggerE_QuotaHigh(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	hub := events.NewHub(50)
	q := newFakeQuota()
	s := newFakeSessions("alpha")
	thr := Defaults()
	thr.QuotaPct = 85
	eng := newEngineAt(&now, hub, q, s, thr)

	q.setWeekly(90)
	q.setFiveHr(10)
	eng.evaluateAll()

	snap, ok := eng.Snapshot("alpha")
	if !ok || snap.State != StateQuotaHigh {
		t.Fatalf("want quota_high (weekly), got %+v ok=%v", snap, ok)
	}

	// Drop weekly; 5h still low → clears.
	q.setWeekly(50)
	eng.evaluateAll()
	if _, ok := eng.Snapshot("alpha"); ok {
		t.Fatalf("expected clear after quota drop")
	}

	// 5-hour-side also fires independently.
	q.setFiveHr(86)
	eng.evaluateAll()
	if snap, ok := eng.Snapshot("alpha"); !ok || snap.State != StateQuotaHigh {
		t.Fatalf("want quota_high (5h), got %+v ok=%v", snap, ok)
	}
}

func TestTriggerF_ContextImminent(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	hub := events.NewHub(50)
	q := newFakeQuota()
	s := newFakeSessions("alpha")
	thr := Defaults()
	thr.ContextPct = 90
	eng := newEngineAt(&now, hub, q, s, thr)

	q.setCtx("alpha", 92)
	eng.evaluateAll()

	snap, ok := eng.Snapshot("alpha")
	if !ok || snap.State != StateContextImminent {
		t.Fatalf("want context_imminent, got %+v ok=%v", snap, ok)
	}

	q.setCtx("alpha", 50)
	eng.evaluateAll()
	if _, ok := eng.Snapshot("alpha"); ok {
		t.Fatalf("expected clear after context drop")
	}
}

func TestTriggerG_YoloUnchecked(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	hub := events.NewHub(50)
	q := newFakeQuota()
	s := newFakeSessions("alpha")
	thr := Defaults()
	thr.YoloUncheckedMinutes = 30
	eng := newEngineAt(&now, hub, q, s, thr)

	s.setMode("alpha", "yolo")
	// Initial tick: yoloAt recorded; not tripped (brand new).
	eng.evaluateAll()
	if _, ok := eng.Snapshot("alpha"); ok {
		t.Fatalf("G must not trip on fresh yolo")
	}

	// 31 minutes later, still no checkpoint → fires.
	now = now.Add(31 * time.Minute)
	eng.evaluateAll()
	snap, ok := eng.Snapshot("alpha")
	if !ok || snap.State != StateYoloUnchecked {
		t.Fatalf("want yolo_unchecked, got %+v ok=%v", snap, ok)
	}

	// Recent checkpoint clears.
	s.setCheckpoint("alpha", now.Add(-1*time.Minute))
	eng.evaluateAll()
	if _, ok := eng.Snapshot("alpha"); ok {
		t.Fatalf("expected clear after fresh checkpoint")
	}
}

func TestPrecedence_TmuxDeadBeatsErrorBurst(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	hub := events.NewHub(50)
	q := newFakeQuota()
	s := newFakeSessions("alpha")
	thr := Defaults()
	thr.ErrorRateWindow = 4
	thr.ErrorRatePct = 50
	eng := newEngineAt(&now, hub, q, s, thr)

	// Fill window with errors (A fires) and kill tmux — D wins.
	for i := 0; i < 4; i++ {
		eng.handleEvent(toolCallEv(t, "alpha", true, now))
	}
	s.setAlive("alpha", false)
	eng.evaluateAll()

	snap, ok := eng.Snapshot("alpha")
	if !ok || snap.State != StateTmuxDead {
		t.Fatalf("want tmux_dead, got %+v ok=%v", snap, ok)
	}
}

func TestSnapshotFalseWhenClear(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	hub := events.NewHub(50)
	eng := newEngineAt(&now, hub, newFakeQuota(), newFakeSessions("alpha"), Defaults())

	if snap, ok := eng.Snapshot("alpha"); ok {
		t.Fatalf("want (zero, false), got (%+v, %v)", snap, ok)
	}
	// Untracked names also return false.
	if _, ok := eng.Snapshot("ghost"); ok {
		t.Fatalf("want false for unknown session")
	}
}

func TestPublishesRaisedAndCleared(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	hub := events.NewHub(50)
	sub, _ := hub.Subscribe("", "")
	defer sub.Close()

	q := newFakeQuota()
	s := newFakeSessions("alpha")
	eng := newEngineAt(&now, hub, q, s, Defaults())

	// Raise via trigger A.
	eng.handleEvent(toolCallEv(t, "alpha", true, now))
	eng.evaluateAll()

	raised := waitForType(t, sub, "attention_raised", 1*time.Second)
	if raised.Session != "alpha" {
		t.Fatalf("raised session = %q", raised.Session)
	}
	var rp raisedPayload
	if err := json.Unmarshal(raised.Payload, &rp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rp.State != StateLastErrorCall {
		t.Fatalf("raised state = %q", rp.State)
	}

	// Clear via non-error call.
	eng.handleEvent(toolCallEv(t, "alpha", false, now))
	eng.evaluateAll()
	cleared := waitForType(t, sub, "attention_cleared", 1*time.Second)
	if cleared.Session != "alpha" {
		t.Fatalf("cleared session = %q", cleared.Session)
	}
}

func TestIdempotentNoRepublish(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	hub := events.NewHub(50)
	sub, _ := hub.Subscribe("", "")
	defer sub.Close()

	q := newFakeQuota()
	s := newFakeSessions("alpha")
	eng := newEngineAt(&now, hub, q, s, Defaults())

	q.setWeekly(95)
	eng.evaluateAll()
	_ = waitForType(t, sub, "attention_raised", 1*time.Second)

	// Same state should not re-publish.
	eng.evaluateAll()
	eng.evaluateAll()
	select {
	case ev := <-sub.Events():
		t.Fatalf("unexpected republish: %+v", ev)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestRunReturnsOnContextCancel(t *testing.T) {
	hub := events.NewHub(50)
	eng := NewEngine(hub, newFakeQuota(), newFakeSessions("alpha"), Defaults(), nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- eng.Run(ctx) }()

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned error: %v", err)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("Run did not return within 50ms of ctx cancel")
	}
}

func TestDropSafe_StateStillUpdates(t *testing.T) {
	// A slow subscriber gets events dropped by the hub; engine state
	// must still reflect the alert because evaluation mutates state
	// BEFORE publish, and publish is fire-and-forget.
	now := time.Unix(1_700_000_000, 0).UTC()
	hub := events.NewHub(2) // tiny ring — easy to overflow sub channel
	// Subscribe but never read: force drops.
	sub, _ := hub.Subscribe("", "")
	defer sub.Close()

	q := newFakeQuota()
	s := newFakeSessions("alpha")
	eng := newEngineAt(&now, hub, q, s, Defaults())

	// Push many alerts without draining sub.
	for i := 0; i < 500; i++ {
		eng.handleEvent(toolCallEv(t, "alpha", true, now))
		eng.evaluateAll()
	}

	snap, ok := eng.Snapshot("alpha")
	if !ok || snap.State != StateLastErrorCall {
		t.Fatalf("state lost under drop pressure: %+v ok=%v", snap, ok)
	}
}

// waitForType drains sub until a matching event arrives or timeout expires.
// Ignores synthetic hub-replay entries that don't match.
func waitForType(t *testing.T, sub *events.Sub, typ string, timeout time.Duration) events.Event {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case ev, ok := <-sub.Events():
			if !ok {
				t.Fatalf("sub closed waiting for %q", typ)
			}
			if ev.Type == typ {
				return ev
			}
		case <-deadline:
			t.Fatalf("timeout waiting for %q", typ)
		}
	}
}
