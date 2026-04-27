package serve

import (
	"context"
	"errors"
	"testing"
	"time"
)

// scriptedCapturer returns successive strings from a scripted slice.
// Once the slice is exhausted it keeps returning the final entry
// (mirrors a pane that has stabilised).
type scriptedCapturer struct {
	outputs []string
	calls   int
	err     error
}

func (s *scriptedCapturer) CapturePane(_ string) (string, error) {
	idx := s.calls
	s.calls++
	if s.err != nil {
		return "", s.err
	}
	if idx >= len(s.outputs) {
		return s.outputs[len(s.outputs)-1], nil
	}
	return s.outputs[idx], nil
}

// churnCapturer returns a never-stable stream of distinct strings so
// the helper can never see two consecutive identical captures.
type churnCapturer struct {
	calls int
}

func (c *churnCapturer) CapturePane(_ string) (string, error) {
	c.calls++
	// Each call returns a unique string.
	return time.Unix(int64(c.calls), 0).String(), nil
}

// fakeClock produces a monotonically advancing time driven by sleep
// calls, so tests don't actually block.
type fakeClock struct {
	now time.Time
}

func (f *fakeClock) Now() time.Time { return f.now }

func (f *fakeClock) Sleep(d time.Duration) { f.now = f.now.Add(d) }

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(1_700_000_000, 0)}
}

func TestWaitForPaneReady_ReadyQuickly(t *testing.T) {
	clock := newFakeClock()
	cap := &scriptedCapturer{outputs: []string{"claude> ", "claude> "}}

	err := waitForPaneReady(context.Background(), cap, "sess", waitOpts{
		interval: 200 * time.Millisecond,
		timeout:  15 * time.Second,
		now:      clock.Now,
		sleep:    clock.Sleep,
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if cap.calls != 2 {
		t.Fatalf("expected 2 capture calls, got %d", cap.calls)
	}
}

func TestWaitForPaneReady_StabilizesAfterChurn(t *testing.T) {
	clock := newFakeClock()
	cap := &scriptedCapturer{outputs: []string{
		"booting",
		"booting...",
		"claude> ",
		"claude> ",
	}}

	err := waitForPaneReady(context.Background(), cap, "sess", waitOpts{
		interval: 200 * time.Millisecond,
		timeout:  15 * time.Second,
		now:      clock.Now,
		sleep:    clock.Sleep,
	})
	if err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
	// First pair of identical non-empty captures arrives at call 4.
	if cap.calls != 4 {
		t.Fatalf("expected exactly 4 capture calls, got %d", cap.calls)
	}
}

func TestWaitForPaneReady_EmptyNeverReady(t *testing.T) {
	clock := newFakeClock()
	cap := &scriptedCapturer{outputs: []string{"", "", ""}}

	err := waitForPaneReady(context.Background(), cap, "sess", waitOpts{
		interval: 200 * time.Millisecond,
		timeout:  1 * time.Second,
		now:      clock.Now,
		sleep:    clock.Sleep,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
	// Must have made at least a handful of attempts across the budget.
	if cap.calls < 3 {
		t.Fatalf("expected multiple capture attempts, got %d", cap.calls)
	}
}

func TestWaitForPaneReady_Timeout(t *testing.T) {
	clock := newFakeClock()
	cap := &churnCapturer{}

	err := waitForPaneReady(context.Background(), cap, "sess", waitOpts{
		interval: 200 * time.Millisecond,
		timeout:  1 * time.Second,
		now:      clock.Now,
		sleep:    clock.Sleep,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected DeadlineExceeded, got %v", err)
	}
	if cap.calls < 3 {
		t.Fatalf("expected multiple capture attempts during churn, got %d", cap.calls)
	}
}
