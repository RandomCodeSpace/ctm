package events

import (
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultRingSize = 500
	subChanBuffer   = 128
	dropLogInterval = time.Minute
)

// ring is a fixed-capacity FIFO of events keyed insertion-order. Oldest
// entry is at index head; size grows to cap then wraps.
type ring struct {
	buf  []Event
	head int
	size int
	cap  int
}

func newRing(cap int) *ring { return &ring{buf: make([]Event, cap), cap: cap} }

func (r *ring) push(e Event) {
	if r.cap == 0 {
		return
	}
	idx := (r.head + r.size) % r.cap
	r.buf[idx] = e
	if r.size < r.cap {
		r.size++
	} else {
		r.head = (r.head + 1) % r.cap
	}
}

// snapshot returns events in chronological order.
func (r *ring) snapshot() []Event {
	out := make([]Event, r.size)
	for i := 0; i < r.size; i++ {
		out[i] = r.buf[(r.head+i)%r.cap]
	}
	return out
}

// oldestID returns the ID of the oldest entry or "" if empty.
func (r *ring) oldestID() string {
	if r.size == 0 {
		return ""
	}
	return r.buf[r.head].ID
}

// Sub is a hub subscription. Consumers read events off Events(); the hub
// drops events for this sub when its channel is full.
type Sub struct {
	ch       chan Event
	filter   string
	closed   atomic.Bool
	dropped  atomic.Uint64
	hub      *Hub
	lastWarn atomic.Int64 // unix-nano of most recent drop WARN

	closeOnce sync.Once
}

// Events returns the receive channel. The channel is closed when Close
// is called or the hub is shut down.
func (s *Sub) Events() <-chan Event { return s.ch }

// Dropped returns the number of events that were dropped for this sub
// because its channel was full when the publisher tried to enqueue.
func (s *Sub) Dropped() uint64 { return s.dropped.Load() }

// Close removes the sub from the hub and closes its channel. Idempotent.
func (s *Sub) Close() {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		if s.hub != nil {
			s.hub.removeSub(s)
		}
		close(s.ch)
	})
}

// Stats describes hub state for /health.
type Stats struct {
	Published   uint64         `json:"published"`
	Dropped     uint64         `json:"dropped"`
	Subscribers int            `json:"subscribers"`
	RingSizes   map[string]int `json:"ring_sizes"`
}

// Hub is the in-process pub/sub fan-out plus per-session ring buffer.
type Hub struct {
	mu       sync.RWMutex
	subs     map[*Sub]struct{}
	rings    map[string]*ring
	ringSize int

	published atomic.Uint64

	// idSeq counts events emitted within the current second, used as the
	// monotonic suffix on Event.ID. Reset when the unix-second changes.
	idMu     sync.Mutex
	idLastNs int64
	idSeq    uint64
}

// NewHub returns a hub with the given per-ring capacity. ringSize <= 0
// uses the default (500).
func NewHub(ringSize int) *Hub {
	if ringSize <= 0 {
		ringSize = defaultRingSize
	}
	return &Hub{
		subs:     make(map[*Sub]struct{}),
		rings:    make(map[string]*ring),
		ringSize: ringSize,
	}
}

// nextID assigns a monotonically-increasing "<unix-nano>-<seq>" id.
// Within the same nanosecond the seq increments; otherwise it resets.
func (h *Hub) nextID(now time.Time) string {
	h.idMu.Lock()
	defer h.idMu.Unlock()
	ns := now.UnixNano()
	if ns <= h.idLastNs {
		// Same instant or clock didn't tick; bump seq and reuse last ns
		// so monotonicity holds even under coarse clocks.
		h.idSeq++
		ns = h.idLastNs
	} else {
		h.idLastNs = ns
		h.idSeq = 0
	}
	return strconv.FormatInt(ns, 10) + "-" + strconv.FormatUint(h.idSeq, 10)
}

// Publish appends e to the appropriate rings and fans out to subscribers.
// Never blocks: a full subscriber channel causes the event to be dropped
// for that sub (drop counter incremented; WARN logged at most once/min).
func (h *Hub) Publish(e Event) {
	now := time.Now()
	if e.ts.IsZero() {
		e.ts = now
	}
	if e.ID == "" {
		e.ID = h.nextID(now)
	}

	h.mu.Lock()
	h.appendRing(globalRing, e)
	if e.Session != "" {
		h.appendRing(e.Session, e)
	}
	// Snapshot subs while holding the lock; deliver outside.
	subs := make([]*Sub, 0, len(h.subs))
	for s := range h.subs {
		subs = append(subs, s)
	}
	h.mu.Unlock()

	h.published.Add(1)

	for _, s := range subs {
		if s.closed.Load() {
			continue
		}
		if s.filter != "" && s.filter != e.Session {
			continue
		}
		select {
		case s.ch <- e:
		default:
			n := s.dropped.Add(1)
			h.maybeWarnDrop(s, n)
		}
	}
}

func (h *Hub) appendRing(key string, e Event) {
	r := h.rings[key]
	if r == nil {
		r = newRing(h.ringSize)
		h.rings[key] = r
	}
	r.push(e)
}

func (h *Hub) maybeWarnDrop(s *Sub, total uint64) {
	now := time.Now().UnixNano()
	prev := s.lastWarn.Load()
	if prev != 0 && now-prev < int64(dropLogInterval) {
		return
	}
	if !s.lastWarn.CompareAndSwap(prev, now) {
		return
	}
	slog.Warn("hub subscriber dropping events",
		"filter", s.filter,
		"dropped_total", total)
}

// Subscribe registers a new subscriber and returns it together with the
// replay slice from the appropriate ring.
//
// filter == "" subscribes to the global stream (every event); a non-empty
// filter restricts delivery to events whose Session matches.
//
// since is a Last-Event-ID value: the replay slice contains buffered
// events strictly after that ID, in chronological order. since == ""
// returns an empty slice (start live). If since predates the ring's
// oldest entry the entire ring snapshot is returned and Lost reports
// true so the caller can emit a "lost" marker to the SSE client.
func (h *Hub) Subscribe(filter, since string) (*Sub, []Event) {
	sub, ev, _ := h.subscribe(filter, since)
	return sub, ev
}

// subscribe is the internal variant exposing the lost-gap flag.
func (h *Hub) subscribe(filter, since string) (*Sub, []Event, bool) {
	s := &Sub{
		ch:     make(chan Event, subChanBuffer),
		filter: filter,
		hub:    h,
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	h.subs[s] = struct{}{}

	key := globalRing
	if filter != "" {
		key = filter
	}
	r := h.rings[key]
	if r == nil {
		return s, nil, false
	}

	snap := r.snapshot()
	if since == "" {
		// Fresh connect with no resume cursor: seed the subscriber
		// with the full ring snapshot so UI state (feed scrollback,
		// quota bars, session cards) survives a page reload instead
		// of staring at "waiting for first event…" until the next
		// publish. Not treated as a gap — the client never had a
		// cursor in the first place, so there's nothing "lost".
		return s, snap, false
	}
	if idLessThan(since, r.oldestID()) {
		// Gap unfillable — caller should emit a "lost" marker.
		return s, snap, true
	}
	// Find first event with ID > since.
	idx := -1
	for i, e := range snap {
		if idLessThan(since, e.ID) {
			idx = i
			break
		}
	}
	if idx == -1 {
		return s, nil, false
	}
	out := make([]Event, len(snap)-idx)
	copy(out, snap[idx:])
	return s, out, false
}

// removeSub is invoked by Sub.Close to detach from the hub.
func (h *Hub) removeSub(s *Sub) {
	h.mu.Lock()
	delete(h.subs, s)
	remaining := len(h.subs)
	h.mu.Unlock()
	slog.Info("hub unsubscribe",
		"filter", s.filter, "dropped", s.dropped.Load(),
		"subscribers_after", remaining)
}

// Snapshot returns a chronological copy of the ring for filter
// (empty string = global ring). Caller owns the returned slice.
// Used by REST seed endpoints (/api/feed, /api/sessions/{name}/feed)
// so first paint renders historical events immediately — hub.Subscribe
// returns no replay on empty Last-Event-ID, and we don't want fresh
// browser tabs to stare at an empty list until the next publish.
func (h *Hub) Snapshot(filter string) []Event {
	key := globalRing
	if filter != "" {
		key = filter
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	r := h.rings[key]
	if r == nil {
		return nil
	}
	return r.snapshot()
}

// Stats returns a point-in-time snapshot of hub counters and ring sizes.
func (h *Hub) Stats() Stats {
	h.mu.RLock()
	defer h.mu.RUnlock()
	rings := make(map[string]int, len(h.rings))
	var dropped uint64
	for k, r := range h.rings {
		rings[k] = r.size
	}
	for s := range h.subs {
		dropped += s.dropped.Load()
	}
	return Stats{
		Published:   h.published.Load(),
		Dropped:     dropped,
		Subscribers: len(h.subs),
		RingSizes:   rings,
	}
}

// idLessThan reports whether a < b under the "<unix-nano>-<seq>" ID
// ordering. Falls back to lexicographic for malformed IDs (since the
// numeric prefixes are zero-padded by FormatInt of 19-digit nanos and
// the seq is small, lexical ordering matches numeric ordering for
// equal-length prefixes; we split to be safe).
//
// Exported for sse.go test reuse.
func idLessThan(a, b string) bool {
	an, as := splitID(a)
	bn, bs := splitID(b)
	if an != bn {
		return an < bn
	}
	return as < bs
}

func splitID(id string) (int64, uint64) {
	left, right, ok := strings.Cut(id, "-")
	if !ok {
		return 0, 0
	}
	ns, _ := strconv.ParseInt(left, 10, 64)
	seq, _ := strconv.ParseUint(right, 10, 64)
	return ns, seq
}
