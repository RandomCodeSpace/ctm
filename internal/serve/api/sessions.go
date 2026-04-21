// Package api hosts the HTTP handlers for ctm serve. Each file in this
// package owns one resource family; this file owns /api/sessions and
// /api/sessions/{name}.
//
// Wiring lives in internal/serve/server.go (route registration is the
// caller's responsibility). Handlers here return enriched Session views
// per spec §6: fields the projection cannot yet populate
// (last_tool_call_at, context_pct, attention) are sourced through the
// SessionEnricher interface and OMITTED from the JSON when the
// enricher reports no value. Later steps in the ctm-serve plan will
// supply real enricher implementations.
package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/ingest"
	"github.com/RandomCodeSpace/ctm/internal/session"
)

// Attention mirrors the spec §6 Session.attention sub-object. Values
// come from a future attention engine; for now the enricher always
// reports "no value" and the field is omitted.
type Attention struct {
	State   string    `json:"state"`
	Since   time.Time `json:"since"`
	Details string    `json:"details,omitempty"`
}

// SessionEnricher supplies the per-session fields that the sessions
// projection cannot derive on its own. Implementations return ok=false
// to signal "no value yet"; handlers omit those fields from the JSON.
//
// Stable interface — later steps plug in real implementations
// (tool-call tailer, statusline-dump quota ingest, attention engine)
// without changing this signature.
type SessionEnricher interface {
	LastToolCallAt(name string) (time.Time, bool)
	ContextPct(name string) (int, bool)
	Attention(name string) (Attention, bool)
	// Tokens returns the live per-session token breakdown from the
	// last statusline dump's current_usage. ok=false means no dump
	// has been ingested yet for this session.
	Tokens(name string) (TokenUsage, bool)
}

// TokenUsage mirrors the statusline dump's `context_window.current_usage`
// payload the ingester captures per session. All three fields are
// current-turn counts, not cumulative session totals.
type TokenUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
	// CacheTokens is creation + read from the statusline dump; the UI
	// doesn't distinguish, so we collapse them at ingest time.
	CacheTokens int `json:"cache_tokens"`
}

// NoopEnricher reports no values for every field. Useful as the default
// while the underlying ingestors are still being built.
type NoopEnricher struct{}

func (NoopEnricher) LastToolCallAt(string) (time.Time, bool) { return time.Time{}, false }
func (NoopEnricher) ContextPct(string) (int, bool)           { return 0, false }
func (NoopEnricher) Attention(string) (Attention, bool)      { return Attention{}, false }
func (NoopEnricher) Tokens(string) (TokenUsage, bool)        { return TokenUsage{}, false }

// sessionView is the spec §6 enriched-view JSON shape. Pointer types
// for the optional enriched fields so we can omit them entirely when
// the enricher has nothing to report (omitempty on a pointer drops the
// key; on a value type it would keep "context_pct":0 — wrong).
type sessionView struct {
	Name             string     `json:"name"`
	UUID             string     `json:"uuid"`
	Mode             string     `json:"mode"`
	Workdir          string     `json:"workdir"`
	CreatedAt        time.Time  `json:"created_at"`
	LastAttachedAt   *time.Time `json:"last_attached_at,omitempty"`
	IsActive         bool       `json:"is_active"`
	TmuxAlive        bool       `json:"tmux_alive"`
	LastToolCallAt   *time.Time  `json:"last_tool_call_at,omitempty"`
	ContextPct       *int        `json:"context_pct,omitempty"`
	Attention        *Attention  `json:"attention,omitempty"`
	Tokens           *TokenUsage `json:"tokens,omitempty"`
}

// buildView projects a session.Session + enrichment + tmux liveness
// into the spec §6 sessionView shape.
func buildView(s session.Session, alive bool, e SessionEnricher) sessionView {
	v := sessionView{
		Name:      s.Name,
		UUID:      s.UUID,
		Mode:      s.Mode,
		Workdir:   s.Workdir,
		CreatedAt: s.CreatedAt,
		// is_active in v0.1: present in our books AND tmux confirms it.
		// Reconciliation against tmux happens elsewhere; for the read
		// model, "is_active" simply tracks tmux liveness.
		IsActive:  alive,
		TmuxAlive: alive,
	}
	if !s.LastAttachedAt.IsZero() {
		t := s.LastAttachedAt
		v.LastAttachedAt = &t
	}
	if t, ok := e.LastToolCallAt(s.Name); ok {
		v.LastToolCallAt = &t
	}
	if pct, ok := e.ContextPct(s.Name); ok {
		v.ContextPct = &pct
	}
	if att, ok := e.Attention(s.Name); ok {
		a := att
		v.Attention = &a
	}
	if t, ok := e.Tokens(s.Name); ok {
		tu := t
		v.Tokens = &tu
	}
	return v
}

// List returns GET /api/sessions — the full sessionView slice.
func List(p *ingest.Projection, e SessionEnricher) http.HandlerFunc {
	if e == nil {
		e = NoopEnricher{}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		all := p.All()
		out := make([]sessionView, 0, len(all))
		for _, s := range all {
			out = append(out, buildView(s, p.TmuxAlive(s.Name), e))
		}
		writeJSON(w, http.StatusOK, out)
	}
}

// Get returns GET /api/sessions/{name} — a single sessionView, or 404.
// Uses the Go 1.22+ http.ServeMux path-pattern variable {name}.
func Get(p *ingest.Projection, e SessionEnricher) http.HandlerFunc {
	if e == nil {
		e = NoopEnricher{}
	}
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		s, ok := p.Get(name)
		if !ok {
			writeJSON(w, http.StatusNotFound, errorBody{Error: "session not found", Name: name})
			return
		}
		writeJSON(w, http.StatusOK, buildView(s, p.TmuxAlive(name), e))
	}
}

// errorBody is the small JSON shape for 4xx responses from this file.
// Kept local to avoid premature shared-types ceremony — the wider error
// model lives in spec §7 and will be unified in a later step.
type errorBody struct {
	Error string `json:"error"`
	Name  string `json:"name,omitempty"`
}

// writeJSON writes v as JSON with the conventions used by the rest of
// internal/serve/api: application/json, no-store cache header, and a
// trailing newline. Errors during marshal degrade to a 500 with the
// error string — these handlers serialize plain structs, so marshal
// failure is effectively impossible in practice.
func writeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"marshal failed"}` + "\n"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(append(body, '\n'))
}
