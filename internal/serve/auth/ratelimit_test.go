package auth_test

import (
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/auth"
)

// newClock returns a clock closure and a setter so tests can advance it.
func newClock(start time.Time) (func() time.Time, func(time.Duration)) {
	now := start
	return func() time.Time { return now }, func(d time.Duration) { now = now.Add(d) }
}

func TestLimiter_AllowsUpToMax(t *testing.T) {
	clk, _ := newClock(time.Unix(1_700_000_000, 0))
	lim := auth.NewLimiterWithClock(5, 60*time.Second, clk)
	for i := 0; i < 5; i++ {
		ok, ra := lim.Allow("1.2.3.4")
		if !ok {
			t.Fatalf("attempt %d denied, want allowed", i+1)
		}
		if ra != 0 {
			t.Fatalf("attempt %d retryAfter = %v, want 0", i+1, ra)
		}
	}
	ok, ra := lim.Allow("1.2.3.4")
	if ok {
		t.Fatalf("6th attempt allowed, want denied")
	}
	if ra <= 0 {
		t.Fatalf("retryAfter = %v, want > 0", ra)
	}
}

func TestLimiter_WindowRollOff(t *testing.T) {
	clk, advance := newClock(time.Unix(1_700_000_000, 0))
	lim := auth.NewLimiterWithClock(5, 60*time.Second, clk)
	for i := 0; i < 5; i++ {
		if ok, _ := lim.Allow("1.2.3.4"); !ok {
			t.Fatalf("attempt %d denied", i+1)
		}
	}
	// 6th within window: denied
	if ok, _ := lim.Allow("1.2.3.4"); ok {
		t.Fatal("6th within window allowed, want denied")
	}
	// Advance past window; all prior attempts should age out.
	advance(61 * time.Second)
	ok, ra := lim.Allow("1.2.3.4")
	if !ok {
		t.Fatalf("post-window attempt denied, retryAfter=%v", ra)
	}
}

func TestLimiter_Reset(t *testing.T) {
	clk, _ := newClock(time.Unix(1_700_000_000, 0))
	lim := auth.NewLimiterWithClock(5, 60*time.Second, clk)
	for i := 0; i < 5; i++ {
		lim.Allow("1.2.3.4")
	}
	if ok, _ := lim.Allow("1.2.3.4"); ok {
		t.Fatal("6th attempt allowed before reset")
	}
	lim.Reset("1.2.3.4")
	for i := 0; i < 5; i++ {
		if ok, _ := lim.Allow("1.2.3.4"); !ok {
			t.Fatalf("attempt %d after reset denied", i+1)
		}
	}
}

func TestLimiter_IndependentIPs(t *testing.T) {
	clk, _ := newClock(time.Unix(1_700_000_000, 0))
	lim := auth.NewLimiterWithClock(5, 60*time.Second, clk)
	for i := 0; i < 5; i++ {
		lim.Allow("1.1.1.1")
	}
	if ok, _ := lim.Allow("1.1.1.1"); ok {
		t.Fatal("1.1.1.1 6th allowed")
	}
	// Different IP still has full budget.
	for i := 0; i < 5; i++ {
		if ok, _ := lim.Allow("2.2.2.2"); !ok {
			t.Fatalf("2.2.2.2 attempt %d denied", i+1)
		}
	}
}

func TestLimiter_DefaultConstructorUsesWallClock(t *testing.T) {
	lim := auth.NewLimiter(2, time.Minute)
	if ok, _ := lim.Allow("x"); !ok {
		t.Fatal("first allow denied")
	}
	if ok, _ := lim.Allow("x"); !ok {
		t.Fatal("second allow denied")
	}
	if ok, _ := lim.Allow("x"); ok {
		t.Fatal("third allow should be denied")
	}
}
