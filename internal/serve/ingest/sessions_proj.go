// Package ingest builds the in-memory projections that the serve API
// reads from. The sessions projection mirrors ~/.config/ctm/sessions.json
// (decoded leniently — serve is a consumer; the CLI owns strictness via
// internal/jsonstrict) and exposes thread-safe accessors plus a tmux
// liveness probe with a short TTL cache.
//
// Polling note: this initial implementation re-reads sessions.json on a
// 1 s ticker whenever the file's mtime changes. Step 5 of the
// ctm-serve plan will swap the polling loop for an fsnotify watcher.
// The public API of Projection (New / Run / All / Get / TmuxAlive) will
// not change when that swap happens.
package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/session"
)

// TmuxClient is the narrow surface of *tmux.Client that the projection
// depends on. Defined here so tests can supply a fake without spinning
// up tmux.
type TmuxClient interface {
	HasSession(name string) bool
}

// tmuxAliveTTL is how long a positive or negative tmux liveness result
// stays cached before the next probe. Trades some staleness for not
// fork/exec-ing tmux on every API request.
const tmuxAliveTTL = 5 * time.Second

// pollInterval is how often Run wakes up to check the file's mtime.
// Cheap (one stat call) and bounded — replaced by fsnotify in step 5.
const pollInterval = 1 * time.Second

type tmuxCacheEntry struct {
	alive    bool
	cachedAt time.Time
}

// Projection is an RWMutex-guarded in-memory snapshot of sessions.json
// plus a tiny TTL cache for tmux liveness probes.
type Projection struct {
	path string
	tmux TmuxClient

	// now is a clock-injection seam used by tests to fast-forward the
	// tmux liveness TTL without sleeping. Defaults to time.Now.
	now func() time.Time

	mu       sync.RWMutex
	sessions []session.Session
	byName   map[string]session.Session
	mtime    time.Time

	tmuxMu    sync.RWMutex
	tmuxCache map[string]tmuxCacheEntry
}

// New constructs a Projection bound to path and the given tmux client.
// Run must be called to populate the snapshot and keep it fresh.
func New(path string, tmux TmuxClient) *Projection {
	return &Projection{
		path:      path,
		tmux:      tmux,
		now:       time.Now,
		byName:    make(map[string]session.Session),
		tmuxCache: make(map[string]tmuxCacheEntry),
	}
}

// Reload synchronously re-reads sessions.json. Call once at startup
// before iterating All() so the snapshot is populated before any
// caller (e.g. serve's tailer-spawn loop) reads from it; otherwise
// they race with Run's first refresh and see an empty list.
func (p *Projection) Reload() {
	p.refresh()
}

// Run loads the initial snapshot and then polls path's mtime every
// pollInterval, re-reading on change. Returns nil when ctx is cancelled.
func (p *Projection) Run(ctx context.Context) error {
	p.refresh() // best-effort initial load; missing file is fine.

	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			p.refresh()
		}
	}
}

// refresh stats the file; if mtime changed (or first load), it
// re-decodes leniently and atomically swaps in the new snapshot.
func (p *Projection) refresh() {
	info, err := os.Stat(p.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Empty out the projection so callers see no sessions
			// rather than stale ones if the file disappears.
			p.mu.Lock()
			if len(p.sessions) > 0 || len(p.byName) > 0 {
				p.sessions = nil
				p.byName = make(map[string]session.Session)
				p.mtime = time.Time{}
			}
			p.mu.Unlock()
			return
		}
		slog.Warn("sessions projection: stat failed", "path", p.path, "err", err)
		return
	}

	p.mu.RLock()
	prev := p.mtime
	p.mu.RUnlock()
	if !info.ModTime().After(prev) && !prev.IsZero() {
		return
	}

	data, err := os.ReadFile(p.path)
	if err != nil {
		slog.Warn("sessions projection: read failed", "path", p.path, "err", err)
		return
	}

	// Lenient decode: tolerate unknown fields and schema drift. This is
	// the deliberate counterpart to the CLI's jsonstrict load — serve
	// must keep working even if sessions.json gains new fields ahead of
	// a serve rebuild.
	var d diskShape
	if err := json.Unmarshal(data, &d); err != nil {
		slog.Warn("sessions projection: lenient decode failed", "path", p.path, "err", err)
		return
	}

	list := make([]session.Session, 0, len(d.Sessions))
	idx := make(map[string]session.Session, len(d.Sessions))
	for _, s := range d.Sessions {
		if s == nil {
			continue
		}
		list = append(list, *s)
		idx[s.Name] = *s
	}

	p.mu.Lock()
	p.sessions = list
	p.byName = idx
	p.mtime = info.ModTime()
	p.mu.Unlock()
}

// diskShape mirrors session.diskData but is lenient: no jsonstrict, and
// any unknown top-level fields are ignored by encoding/json by default.
type diskShape struct {
	SchemaVersion int                          `json:"schema_version"`
	Sessions      map[string]*session.Session  `json:"sessions"`
}

// All returns a defensive copy of the current snapshot. Callers may
// mutate the returned slice without affecting the projection.
func (p *Projection) All() []session.Session {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]session.Session, len(p.sessions))
	copy(out, p.sessions)
	return out
}

// Get returns the session with the given name, if known.
func (p *Projection) Get(name string) (session.Session, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	s, ok := p.byName[name]
	return s, ok
}

// TmuxAlive reports whether tmux currently has a session matching name.
// Results are cached for tmuxAliveTTL to avoid fork/exec on every API
// request.
func (p *Projection) TmuxAlive(name string) bool {
	now := p.now()

	p.tmuxMu.RLock()
	entry, ok := p.tmuxCache[name]
	p.tmuxMu.RUnlock()
	if ok && now.Sub(entry.cachedAt) < tmuxAliveTTL {
		return entry.alive
	}

	alive := p.tmux.HasSession(name)

	p.tmuxMu.Lock()
	p.tmuxCache[name] = tmuxCacheEntry{alive: alive, cachedAt: now}
	p.tmuxMu.Unlock()

	return alive
}
