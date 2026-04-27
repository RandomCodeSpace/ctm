package auth

// Per-IP sliding-window rate limiter for /api/auth/login.
// Rationale: argon2id is deliberately CPU-heavy, so unbounded login
// attempts are a DoS vector. Lazy eviction keeps memory bounded to
// active IPs; successful logins should Reset() to avoid locking out
// legitimate users after typos.

import (
	"sync"
	"time"
)

// Limiter tracks recent timestamps per IP and allows at most max
// attempts within window. Safe for concurrent use.
type Limiter struct {
	max    int
	window time.Duration
	now    func() time.Time

	mu   sync.Mutex
	hits map[string][]time.Time
}

// NewLimiter returns a Limiter using the wall clock.
func NewLimiter(max int, window time.Duration) *Limiter {
	return NewLimiterWithClock(max, window, time.Now)
}

// NewLimiterWithClock returns a Limiter with an injectable clock.
// The clock must be monotonic-ish within the window (tests inject
// a closure that advances deterministically).
func NewLimiterWithClock(max int, window time.Duration, now func() time.Time) *Limiter {
	if now == nil {
		now = time.Now
	}
	return &Limiter{
		max:    max,
		window: window,
		now:    now,
		hits:   make(map[string][]time.Time),
	}
}

// Allow records an attempt for ip and reports whether it is within
// budget. If denied, retryAfter is the duration until the oldest
// in-window attempt ages out.
func (l *Limiter) Allow(ip string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	cutoff := now.Add(-l.window)

	// Lazy-evict expired timestamps for this IP.
	hits := l.hits[ip]
	kept := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.max {
		// Reject without recording; retry-after = time until oldest
		// kept attempt leaves the window.
		retry := kept[0].Add(l.window).Sub(now)
		if retry < 0 {
			retry = 0
		}
		l.hits[ip] = kept
		return false, retry
	}
	kept = append(kept, now)
	l.hits[ip] = kept
	return true, 0
}

// Reset clears all recorded attempts for ip. Called after a
// successful login to avoid locking out a legitimate user who
// mistyped a few times first.
func (l *Limiter) Reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hits, ip)
}
