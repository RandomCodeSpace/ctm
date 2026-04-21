// Package attention implements the v0.1 attention engine: the seven
// locked triggers (A–G) from docs/superpowers/specs/2026-04-20-ctm-serve-
// ui-v0.1-design.md §4 "Attention engine". The engine subscribes to the
// hub's global stream, maintains per-session state, and publishes
// `attention_raised` / `attention_cleared` events on state transitions.
package attention

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/events"
)

// State values are the canonical alert identifiers consumed by the UI
// (see spec §6 Session.attention.state). Empty string means "clear".
const (
	StateClear           = ""
	StateLastErrorCall   = "last_error_call"
	StateErrorBurst      = "error_burst"
	StateStuck           = "stuck"
	StateTmuxDead        = "tmux_dead"
	StateQuotaHigh       = "quota_high"
	StateContextImminent = "context_imminent"
	StateYoloUnchecked   = "yolo_unchecked"
)

// Thresholds wires the seven spec-mandated defaults through to the
// engine. Server-side config maps onto these fields.
type Thresholds struct {
	ErrorRatePct         int
	ErrorRateWindow      int
	IdleMinutes          int
	QuotaPct             int
	ContextPct           int
	YoloUncheckedMinutes int
}

// Defaults returns the thresholds documented in spec §1 "Attention
// triggers". Any field left zero by a caller is NOT backfilled here —
// callers pass `Defaults()` and then override as needed.
func Defaults() Thresholds {
	return Thresholds{
		ErrorRatePct:         20,
		ErrorRateWindow:      20,
		IdleMinutes:          5,
		QuotaPct:             85,
		ContextPct:           90,
		YoloUncheckedMinutes: 30,
	}
}

// Snapshot is the point-in-time per-session view surfaced via the
// SessionEnricher into /api/sessions. An empty State means "no alert".
type Snapshot struct {
	State   string
	Since   time.Time
	Details string
}

// QuotaSource is the read side of ingest.QuotaIngester the engine needs.
// Percentages are float64 to match the underlying accessor; the engine
// compares against the configured int threshold after a plain cast.
type QuotaSource interface {
	WeeklyPct() (float64, bool)
	FiveHourPct() (float64, bool)
	ContextPct(session string) (int, bool)
}

// SessionSource supplies per-session metadata for triggers C/D/G.
type SessionSource interface {
	Names() []string
	Mode(name string) string
	TmuxAlive(name string) bool
	LastCheckpointAt(name string) (time.Time, bool)
}

// tickInterval is how often time-based triggers (C, G) are re-evaluated
// in the absence of events. Short enough to feel live, long enough to
// avoid waking up every session on every heartbeat.
const tickInterval = 30 * time.Second

// Engine is the attention evaluator. A single instance runs per serve
// process, subscribed to the hub's global stream.
type Engine struct {
	hub      *events.Hub
	quota    QuotaSource
	sessions SessionSource
	thr      Thresholds
	now      func() time.Time

	mu       sync.Mutex
	sessions_state map[string]*sessionState
}

// sessionState is the rolling per-session working set. All reads and
// writes go through Engine.mu; Snapshot returns a copy.
type sessionState struct {
	current  Snapshot
	errWin   []bool    // rolling window of is_error, newest last
	lastCall time.Time // timestamp of most recent tool_call seen
	yoloAt   time.Time // when "yolo" mode was first observed
}

// NewEngine constructs an Engine. clock == nil uses time.Now.
func NewEngine(hub *events.Hub, quota QuotaSource, sessions SessionSource, thr Thresholds, clock func() time.Time) *Engine {
	if clock == nil {
		clock = time.Now
	}
	return &Engine{
		hub:            hub,
		quota:          quota,
		sessions:       sessions,
		thr:            thr,
		now:            clock,
		sessions_state: make(map[string]*sessionState),
	}
}

// Run blocks until ctx is cancelled. It drives the engine from two
// sources: hub events (reactive) and a 30-second tick (idle/yolo).
func (e *Engine) Run(ctx context.Context) error {
	if e.hub == nil {
		<-ctx.Done()
		return nil
	}
	sub, replay := e.hub.Subscribe("", "")
	defer sub.Close()

	// Replay the hub ring so a late-starting engine recovers state.
	for _, ev := range replay {
		e.handleEvent(ev)
	}
	e.evaluateAll()

	t := time.NewTicker(tickInterval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-sub.Events():
			if !ok {
				return nil
			}
			// Ignore events we publish ourselves to avoid feedback.
			if ev.Type == "attention_raised" || ev.Type == "attention_cleared" {
				continue
			}
			e.handleEvent(ev)
			e.evaluateAll()
		case <-t.C:
			e.evaluateAll()
		}
	}
}

// Snapshot returns the current per-session snapshot. Returns ok=false
// when no alert is active (StateClear).
func (e *Engine) Snapshot(name string) (Snapshot, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	st, ok := e.sessions_state[name]
	if !ok || st.current.State == StateClear {
		return Snapshot{}, false
	}
	// Copy to decouple from internal mutation.
	return st.current, true
}

// --- event ingestion ------------------------------------------------------

// toolCallPayload mirrors ingest.ToolCallPayload without importing the
// ingest package (keeps the dependency direction clean: attention does
// not depend on ingest).
type toolCallPayload struct {
	Session string    `json:"session"`
	IsError bool      `json:"is_error"`
	TS      time.Time `json:"ts"`
}

// sessionLifecyclePayload decodes the name out of session_new/killed/on_yolo.
type sessionLifecyclePayload struct {
	Name    string `json:"name"`
	Session string `json:"session"`
	Mode    string `json:"mode"`
}

func (e *Engine) handleEvent(ev events.Event) {
	switch ev.Type {
	case "tool_call":
		var p toolCallPayload
		if err := json.Unmarshal(ev.Payload, &p); err != nil {
			return
		}
		name := p.Session
		if name == "" {
			name = ev.Session
		}
		if name == "" {
			return
		}
		ts := p.TS
		if ts.IsZero() {
			ts = e.now()
		}
		e.recordToolCall(name, p.IsError, ts)
	case "session_killed":
		name := sessionNameFromPayload(ev)
		if name != "" {
			e.markTmuxDead(name)
		}
	case "on_yolo":
		name := sessionNameFromPayload(ev)
		if name != "" {
			e.markYolo(name)
		}
	}
}

func sessionNameFromPayload(ev events.Event) string {
	if ev.Session != "" {
		return ev.Session
	}
	var p sessionLifecyclePayload
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		return ""
	}
	if p.Name != "" {
		return p.Name
	}
	return p.Session
}

func (e *Engine) recordToolCall(name string, isError bool, ts time.Time) {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.stateLocked(name)
	st.lastCall = ts
	win := e.thr.ErrorRateWindow
	if win <= 0 {
		win = 1
	}
	st.errWin = append(st.errWin, isError)
	if len(st.errWin) > win {
		st.errWin = st.errWin[len(st.errWin)-win:]
	}
}

func (e *Engine) markTmuxDead(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	// Engine caches the "dead" signal on state; evaluate() will confirm
	// it against SessionSource.TmuxAlive too on the next tick.
	st := e.stateLocked(name)
	// No-op field — tmux_dead is driven from SessionSource.TmuxAlive.
	// This hook is here so we can force-eval on the event.
	_ = st
}

func (e *Engine) markYolo(name string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	st := e.stateLocked(name)
	if st.yoloAt.IsZero() {
		st.yoloAt = e.now()
	}
}

func (e *Engine) stateLocked(name string) *sessionState {
	st, ok := e.sessions_state[name]
	if !ok {
		st = &sessionState{}
		e.sessions_state[name] = st
	}
	return st
}

// --- evaluation -----------------------------------------------------------

// evaluateAll re-computes every known session's state and publishes
// transitions. Called after every event and on every tick.
func (e *Engine) evaluateAll() {
	names := e.sessions.Names()
	seen := make(map[string]struct{}, len(names))
	for _, n := range names {
		seen[n] = struct{}{}
	}

	// Include any sessions the engine is tracking that SessionSource
	// hasn't listed (e.g. a tool_call arrived for a just-forgotten
	// session). They'll resolve to StateClear if no trigger fires.
	e.mu.Lock()
	for n := range e.sessions_state {
		if _, ok := seen[n]; !ok {
			names = append(names, n)
			seen[n] = struct{}{}
		}
	}
	e.mu.Unlock()

	for _, name := range names {
		e.evaluateOne(name)
	}
}

func (e *Engine) evaluateOne(name string) {
	// Observe YOLO mode and remember entry time for trigger G. We do
	// this outside the evaluation lock so SessionSource implementations
	// can hold their own mutexes without risking reentry.
	mode := e.sessions.Mode(name)
	alive := e.sessions.TmuxAlive(name)
	lastCP, hasCP := e.sessions.LastCheckpointAt(name)
	weeklyPct, hasWeekly := e.quota.WeeklyPct()
	fiveHrPct, hasFive := e.quota.FiveHourPct()
	ctxPct, hasCtx := e.quota.ContextPct(name)
	now := e.now()

	e.mu.Lock()
	st := e.stateLocked(name)

	// Track yolo entry once we first observe it.
	if mode == "yolo" {
		if st.yoloAt.IsZero() {
			st.yoloAt = now
		}
	} else {
		st.yoloAt = time.Time{}
	}

	next := e.pickState(st, mode, alive, lastCP, hasCP, weeklyPct, hasWeekly, fiveHrPct, hasFive, ctxPct, hasCtx, now)

	prev := st.current
	var raise, clear bool
	switch {
	case prev.State == next.State && prev.State == StateClear:
		// nothing
	case prev.State == next.State:
		// Keep the original Since; refresh Details if changed.
		if prev.Details != next.Details {
			st.current.Details = next.Details
		}
	case prev.State == StateClear:
		st.current = next
		raise = true
	case next.State == StateClear:
		st.current = Snapshot{}
		clear = true
	default:
		// alert → different alert: clear old, raise new.
		st.current = next
		clear = true
		raise = true
	}

	// Capture what we need for publishing, drop the lock before I/O.
	publishClear := clear
	publishRaise := raise
	prevForClear := prev
	newForRaise := st.current
	e.mu.Unlock()

	if publishClear {
		e.publishCleared(name, prevForClear)
	}
	if publishRaise {
		e.publishRaised(name, newForRaise)
	}
}

// pickState runs the 7 trigger checks and returns the single highest-
// priority alert. Precedence (most urgent first):
//
//	D tmux_dead > A last_error_call > B error_burst > E quota_high >
//	F context_imminent > G yolo_unchecked > C stuck.
//
// The spec is silent on ordering; this matches the severity sort the
// UI list uses (dead/last-error dominate; stuck is the softest signal).
func (e *Engine) pickState(
	st *sessionState,
	mode string,
	alive bool,
	lastCP time.Time, hasCP bool,
	weeklyPct float64, hasWeekly bool,
	fiveHrPct float64, hasFive bool,
	ctxPct int, hasCtx bool,
	now time.Time,
) Snapshot {
	// D · tmux_dead — highest priority: the session is gone.
	if !alive {
		return Snapshot{State: StateTmuxDead, Since: e.sinceOr(st, StateTmuxDead, now), Details: "tmux session no longer exists"}
	}

	// A · last_error_call — most recent tool_call was an error.
	if n := len(st.errWin); n > 0 && st.errWin[n-1] {
		return Snapshot{State: StateLastErrorCall, Since: e.sinceOr(st, StateLastErrorCall, now), Details: "last tool call returned is_error=true"}
	}

	// B · error_burst — rate over rolling window.
	if n := len(st.errWin); n > 0 && e.thr.ErrorRateWindow > 0 && n >= e.thr.ErrorRateWindow {
		errs := 0
		for _, b := range st.errWin {
			if b {
				errs++
			}
		}
		if errs*100 >= e.thr.ErrorRatePct*n {
			return Snapshot{
				State:   StateErrorBurst,
				Since:   e.sinceOr(st, StateErrorBurst, now),
				Details: formatErrorBurst(errs, n),
			}
		}
	}

	// E · quota_high — global weekly OR 5-hour ≥ threshold.
	if (hasWeekly && weeklyPct >= float64(e.thr.QuotaPct)) ||
		(hasFive && fiveHrPct >= float64(e.thr.QuotaPct)) {
		return Snapshot{
			State:   StateQuotaHigh,
			Since:   e.sinceOr(st, StateQuotaHigh, now),
			Details: formatQuota(weeklyPct, hasWeekly, fiveHrPct, hasFive),
		}
	}

	// F · context_imminent — per-session context window.
	if hasCtx && ctxPct >= e.thr.ContextPct {
		return Snapshot{
			State:   StateContextImminent,
			Since:   e.sinceOr(st, StateContextImminent, now),
			Details: formatCtx(ctxPct),
		}
	}

	// G · yolo_unchecked — yolo mode active > threshold without checkpoint.
	if mode == "yolo" && e.thr.YoloUncheckedMinutes > 0 && !st.yoloAt.IsZero() {
		yoloFor := now.Sub(st.yoloAt)
		tooLong := yoloFor > time.Duration(e.thr.YoloUncheckedMinutes)*time.Minute
		noCP := !hasCP || now.Sub(lastCP) > time.Duration(e.thr.YoloUncheckedMinutes)*time.Minute
		if tooLong && noCP {
			return Snapshot{
				State:   StateYoloUnchecked,
				Since:   e.sinceOr(st, StateYoloUnchecked, now),
				Details: "yolo mode without recent checkpoint",
			}
		}
	}

	// C · stuck — idle > threshold while tmux alive.
	if e.thr.IdleMinutes > 0 && !st.lastCall.IsZero() {
		if now.Sub(st.lastCall) > time.Duration(e.thr.IdleMinutes)*time.Minute {
			return Snapshot{
				State:   StateStuck,
				Since:   e.sinceOr(st, StateStuck, now),
				Details: "no tool call in > idle threshold",
			}
		}
	}

	return Snapshot{}
}

// sinceOr keeps the original "since" timestamp when an alert persists
// across evaluations; otherwise returns now.
func (e *Engine) sinceOr(st *sessionState, state string, now time.Time) time.Time {
	if st.current.State == state && !st.current.Since.IsZero() {
		return st.current.Since
	}
	return now
}

// --- publishing -----------------------------------------------------------

type raisedPayload struct {
	Session     string    `json:"session"`
	State       string    `json:"state"`
	Since       time.Time `json:"since"`
	Details     string    `json:"details,omitempty"`
	TriggerRule string    `json:"trigger_rule"`
}

type clearedPayload struct {
	Session string `json:"session"`
	State   string `json:"state,omitempty"`
}

func (e *Engine) publishRaised(name string, s Snapshot) {
	payload, err := json.Marshal(raisedPayload{
		Session:     name,
		State:       s.State,
		Since:       s.Since,
		Details:     s.Details,
		TriggerRule: s.State,
	})
	if err != nil {
		slog.Warn("attention publish raise marshal", "err", err, "session", name)
		return
	}
	e.hub.Publish(events.Event{
		Type:    "attention_raised",
		Session: name,
		Payload: payload,
	})
}

func (e *Engine) publishCleared(name string, prev Snapshot) {
	payload, err := json.Marshal(clearedPayload{Session: name, State: prev.State})
	if err != nil {
		slog.Warn("attention publish clear marshal", "err", err, "session", name)
		return
	}
	e.hub.Publish(events.Event{
		Type:    "attention_cleared",
		Session: name,
		Payload: payload,
	})
}
