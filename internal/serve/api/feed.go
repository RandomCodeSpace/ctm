package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/RandomCodeSpace/ctm/internal/serve/events"
)

// FeedSource is the subset of events.Hub that the Feed handler needs.
// Accepting an interface keeps the api package decoupled from the hub
// for tests and avoids a circular import if events ever grows to
// depend on api.
type FeedSource interface {
	Snapshot(filter string) []events.Event
}

const (
	// defaultFeedLimit caps the REST seed so a freshly-connected
	// browser doesn't render 500 stale rows at once.
	defaultFeedLimit = 200
	// maxFeedLimit bounds caller-supplied ?limit to keep a single
	// request from paying for the full ring (ring.cap is 500).
	maxFeedLimit = 500
)

// Feed returns the GET /api/feed handler (filter == "" → global ring)
// or, when filter is non-empty, GET /api/sessions/{filter}/feed.
//
// Emits ONLY `tool_call` events. Other event types live in the same
// ring (quota_update, attention_*, session lifecycle) but the feed is
// a human-readable tool-call transcript — filtering here keeps the
// contract narrow.
//
// Response shape: array of the same payload the SSE tool_call event
// carries, newest-first ordering so the client does not have to
// reverse when it appends new live events.
//
//	[
//	  {"session":"ctm","tool":"Edit","input":"...","summary":"...",
//	   "is_error":false,"ts":"2026-04-21T14:33:09Z"},
//	  ...
//	]
//
// Returns 200 with an empty array when the ring is empty — lets the
// client distinguish "no history yet" from an error.
func Feed(src FeedSource, filter string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Per-session filter comes from the path variable when
		// mounted at /api/sessions/{name}/feed; otherwise "".
		sessionFilter := filter
		if sessionFilter == "" {
			sessionFilter = r.PathValue("name")
		}

		limit := defaultFeedLimit
		if q := r.URL.Query().Get("limit"); q != "" {
			if n, err := strconv.Atoi(q); err == nil && n > 0 {
				if n > maxFeedLimit {
					n = maxFeedLimit
				}
				limit = n
			}
		}

		all := src.Snapshot(sessionFilter)

		// Walk newest → oldest, keep only tool_call payloads up to
		// limit. Reverse order up front because the UI renders
		// newest-first; the ring stores chronological.
		out := make([]json.RawMessage, 0, limit)
		for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
			ev := all[i]
			if ev.Type != "tool_call" {
				continue
			}
			out = append(out, ev.Payload)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(out)
	}
}
