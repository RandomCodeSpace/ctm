package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/RandomCodeSpace/ctm/internal/serve/events"
)

// QuotaIngester watches the directory where `cmd statusline` drops
// per-session JSON snapshots (path templated by `{uuid}` in
// CTM_STATUSLINE_DUMP) and republishes the aggregated quota state on
// the hub.
//
// Two flavours of `quota_update` event are emitted:
//
//   - Global (`Session == ""`) carrying weekly + 5-hour rate limit
//     percentages. Re-emitted whenever any dump file changes — these
//     fields are not session-scoped, but they're easiest to refresh
//     from the same payload.
//   - Per-session (`Session == <name>`) carrying the per-session
//     `context_pct` derived from the `context_window.used_percentage`
//     field. UUID → session name resolution goes through the
//     Projection.
//
// QuotaIngester also implements the surface a `SessionEnricher` adapter
// in server.go needs to populate the `context_pct` field on the
// /api/sessions view.
type QuotaIngester struct {
	dir  string
	proj *Projection
	hub  *events.Hub

	mu       sync.RWMutex
	perSess  map[string]sessionQuota // keyed on human session name
	global   globalQuota
	hasGlbl  bool
	hasAnySS bool
}

type sessionQuota struct {
	contextPct int
	// inputTokens / outputTokens are the session-cumulative
	// total_{input,output}_tokens from the statusline payload —
	// matches what the physical statusline renders with ↑ and ↓.
	// Grows monotonically through the session.
	inputTokens  int
	outputTokens int
	// cacheTokens is the sum of cache_creation_input_tokens +
	// cache_read_input_tokens from current_usage. No session total
	// is published for cache, so this moves with each turn.
	cacheTokens int
	at          time.Time
}

// SessionTokenSnapshot is the live per-session token view exposed to
// the REST /api/sessions handler. Zero values indicate "unknown" —
// callers gate on the bool from PerSessionSnapshot.
type SessionTokenSnapshot struct {
	ContextPct   int
	InputTokens  int
	OutputTokens int
	CacheTokens  int
}

// globalQuota tracks the most recent rate-limit snapshot. Each field
// has a `has*` companion so a partial statusline dump (e.g. a context-
// only update mid-turn) doesn't clobber known-good values from a prior
// full dump with zeros.
type globalQuota struct {
	weeklyPct         float64
	fiveHrPct         float64
	weeklyResetsAt    time.Time
	fiveHrResetsAt    time.Time
	hasWeeklyPct      bool
	hasFiveHrPct      bool
	hasWeeklyResetsAt bool
	hasFiveHrResetsAt bool
	at                time.Time
}

// NewQuotaIngester constructs an ingester rooted at dir. proj is used
// to resolve UUID → session name; pass nil and per-session events will
// not be published (global rate limits still are).
func NewQuotaIngester(dir string, proj *Projection, hub *events.Hub) *QuotaIngester {
	return &QuotaIngester{
		dir:     dir,
		proj:    proj,
		hub:     hub,
		perSess: make(map[string]sessionQuota),
	}
}

// Run blocks until ctx is cancelled. Re-scans every file in dir on
// startup so a freshly-spawned serve picks up state Claude wrote
// minutes earlier; then watches for file events.
func (q *QuotaIngester) Run(ctx context.Context) error {
	if err := os.MkdirAll(q.dir, 0o700); err != nil {
		return err
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer w.Close()
	if err := w.Add(q.dir); err != nil {
		return err
	}

	// Initial sweep: pick up any dump files that already exist.
	entries, err := os.ReadDir(q.dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		q.ingest(filepath.Join(q.dir, e.Name()))
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-w.Events:
			if !ok {
				return nil
			}
			if !strings.HasSuffix(ev.Name, ".json") {
				continue
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create) != 0 {
				q.ingest(ev.Name)
			}
		case err, ok := <-w.Errors:
			if !ok {
				return nil
			}
			slog.Warn("quota ingest fsnotify error", "err", err)
		}
	}
}

// ingest reads one dump file, derives the per-session UUID from the
// filename (or the payload's session_id field as fallback), updates
// in-memory state, and publishes events on changes.
func (q *QuotaIngester) ingest(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		// File may have been removed between fsnotify and read; ignore.
		return
	}
	if len(data) == 0 {
		return
	}
	var raw struct {
		SessionID     string `json:"session_id"`
		ContextWindow struct {
			UsedPercentage    *float64 `json:"used_percentage"`
			TotalInputTokens  *int     `json:"total_input_tokens"`
			TotalOutputTokens *int     `json:"total_output_tokens"`
			CurrentUsage      struct {
				CacheCreationInputTokens *int `json:"cache_creation_input_tokens"`
				CacheReadInputTokens     *int `json:"cache_read_input_tokens"`
			} `json:"current_usage"`
		} `json:"context_window"`
		RateLimits struct {
			SevenDay struct {
				UsedPercentage *float64 `json:"used_percentage"`
				ResetsAt       *int64   `json:"resets_at"`
			} `json:"seven_day"`
			FiveHour struct {
				UsedPercentage *float64 `json:"used_percentage"`
				ResetsAt       *int64   `json:"resets_at"`
			} `json:"five_hour"`
		} `json:"rate_limits"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}

	uuid := raw.SessionID
	if uuid == "" {
		// Fall back to filename stem (the {uuid} template wrote it).
		stem := strings.TrimSuffix(filepath.Base(path), ".json")
		uuid = stem
	}

	q.mu.Lock()
	prevGlobal := q.global
	hadGlobal := q.hasGlbl
	// Merge incoming fields onto prevGlobal — only overwrite fields
	// the payload actually carried. Partial dumps must not clobber
	// known-good rate-limit state with zeros.
	gl := prevGlobal
	gl.at = time.Now().UTC()
	if p := raw.RateLimits.SevenDay.UsedPercentage; p != nil {
		gl.weeklyPct = *p
		gl.hasWeeklyPct = true
	}
	if p := raw.RateLimits.FiveHour.UsedPercentage; p != nil {
		gl.fiveHrPct = *p
		gl.hasFiveHrPct = true
	}
	if u := raw.RateLimits.SevenDay.ResetsAt; u != nil {
		gl.weeklyResetsAt = time.Unix(*u, 0).UTC()
		gl.hasWeeklyResetsAt = true
	}
	if u := raw.RateLimits.FiveHour.ResetsAt; u != nil {
		gl.fiveHrResetsAt = time.Unix(*u, 0).UTC()
		gl.hasFiveHrResetsAt = true
	}

	q.global = gl
	// hasGlbl flips true the first time ANY rate-limit field is seen;
	// stays true thereafter so accessors keep returning the cached
	// value across context-only dumps.
	if gl.hasWeeklyPct || gl.hasFiveHrPct {
		q.hasGlbl = true
	}
	// Resolve UUID → session name for per-session publish.
	var sessionName string
	if q.proj != nil {
		for _, s := range q.proj.All() {
			if s.UUID == uuid {
				sessionName = s.Name
				break
			}
		}
	}
	var perSessChanged bool
	if sessionName != "" {
		ctxPct := 0
		if p := raw.ContextWindow.UsedPercentage; p != nil {
			ctxPct = int(*p + 0.5)
		}
		// Use the session-cumulative totals (what the statusline renders
		// with ↑ and ↓). The current_usage per-turn counts are tiny
		// (often <100) and never reach the "k" threshold, which reads
		// as broken in the UI. Cache has no cumulative total in the
		// payload, so it stays live (creation + read per turn).
		in := derefInt(raw.ContextWindow.TotalInputTokens)
		out := derefInt(raw.ContextWindow.TotalOutputTokens)
		cache := derefInt(raw.ContextWindow.CurrentUsage.CacheCreationInputTokens) +
			derefInt(raw.ContextWindow.CurrentUsage.CacheReadInputTokens)
		prev, had := q.perSess[sessionName]
		if !had ||
			prev.contextPct != ctxPct ||
			prev.inputTokens != in ||
			prev.outputTokens != out ||
			prev.cacheTokens != cache {
			perSessChanged = true
		}
		q.perSess[sessionName] = sessionQuota{
			contextPct:   ctxPct,
			inputTokens:  in,
			outputTokens: out,
			cacheTokens:  cache,
			at:           time.Now().UTC(),
		}
		q.hasAnySS = true
	}
	q.mu.Unlock()

	// Only publish when we actually have rate-limit data AND something
	// changed. A context-only dump with no rate_limits leaves
	// hasWeeklyPct/hasFiveHrPct false and must NOT publish a fake
	// quota_update with zeros (would flicker the UI bars).
	rateLimitChanged := (gl.hasWeeklyPct && prevGlobal.weeklyPct != gl.weeklyPct) ||
		(gl.hasFiveHrPct && prevGlobal.fiveHrPct != gl.fiveHrPct)
	firstFullDump := !hadGlobal && (gl.hasWeeklyPct || gl.hasFiveHrPct)
	if firstFullDump || rateLimitChanged {
		q.publishGlobal(gl)
	}
	if perSessChanged && sessionName != "" {
		q.publishSession(sessionName)
	}
}

func (q *QuotaIngester) publishGlobal(g globalQuota) {
	body, _ := json.Marshal(map[string]any{
		"weekly_pct":        roundPct(g.weeklyPct),
		"five_hr_pct":       roundPct(g.fiveHrPct),
		"weekly_resets_at":  rfc3339OrEmpty(g.weeklyResetsAt),
		"five_hr_resets_at": rfc3339OrEmpty(g.fiveHrResetsAt),
	})
	slog.Info("quota publish global",
		"weekly_pct", roundPct(g.weeklyPct), "five_hr_pct", roundPct(g.fiveHrPct))
	q.hub.Publish(events.Event{Type: "quota_update", Payload: body})
}

func (q *QuotaIngester) publishSession(name string) {
	q.mu.RLock()
	s := q.perSess[name]
	q.mu.RUnlock()
	body, _ := json.Marshal(map[string]any{
		"session":       name,
		"context_pct":   s.contextPct,
		"input_tokens":  s.inputTokens,
		"output_tokens": s.outputTokens,
		"cache_tokens":  s.cacheTokens,
	})
	slog.Info("quota publish session",
		"session", name, "context_pct", s.contextPct,
		"input", s.inputTokens, "output", s.outputTokens, "cache", s.cacheTokens)
	q.hub.Publish(events.Event{Type: "quota_update", Session: name, Payload: body})
}

// PerSessionSnapshot returns the live token view for a session, or
// (_, false) if no statusline dump has been ingested for it yet.
// Called by the api.SessionEnricher adapter so REST /api/sessions
// renders token counts on first paint.
func (q *QuotaIngester) PerSessionSnapshot(name string) (SessionTokenSnapshot, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	s, ok := q.perSess[name]
	if !ok {
		return SessionTokenSnapshot{}, false
	}
	return SessionTokenSnapshot{
		ContextPct:   s.contextPct,
		InputTokens:  s.inputTokens,
		OutputTokens: s.outputTokens,
		CacheTokens:  s.cacheTokens,
	}, true
}

func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// ContextPct returns the latest context_window.used_percentage seen
// for the named session, rounded to a whole percent.
func (q *QuotaIngester) ContextPct(name string) (int, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	s, ok := q.perSess[name]
	if !ok {
		return 0, false
	}
	return s.contextPct, true
}

// WeeklyPct returns the latest 7-day rate limit percentage, if known.
func (q *QuotaIngester) WeeklyPct() (float64, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if !q.hasGlbl {
		return 0, false
	}
	return q.global.weeklyPct, true
}

// FiveHourPct returns the latest 5-hour rate limit percentage, if known.
func (q *QuotaIngester) FiveHourPct() (float64, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	if !q.hasGlbl {
		return 0, false
	}
	return q.global.fiveHrPct, true
}

// GlobalSnapshot is the point-in-time rate-limit view exposed to the
// REST /api/quota handler. Zero values indicate "unknown" (no dump has
// populated that field yet); callers should gate on Known before
// trusting the percentages.
type GlobalSnapshot struct {
	WeeklyPct       int
	FiveHourPct     int
	WeeklyResetsAt  time.Time
	FiveHourResetAt time.Time
	Known           bool
}

// Snapshot returns the current global rate-limit state under a single
// read lock so the REST response is consistent (vs. calling
// WeeklyPct/FiveHourPct separately and risking a torn read between a
// concurrent ingest).
func (q *QuotaIngester) Snapshot() GlobalSnapshot {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return GlobalSnapshot{
		WeeklyPct:       roundPct(q.global.weeklyPct),
		FiveHourPct:     roundPct(q.global.fiveHrPct),
		WeeklyResetsAt:  q.global.weeklyResetsAt,
		FiveHourResetAt: q.global.fiveHrResetsAt,
		Known:           q.hasGlbl,
	}
}

func roundPct(f float64) int { return int(f + 0.5) }

func rfc3339OrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
