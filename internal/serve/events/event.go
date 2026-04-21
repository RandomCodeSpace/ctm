// Package events implements the in-memory pub/sub hub and SSE handler
// that fan tool-call, quota, and lifecycle events out to the UI.
//
// All routing is in-process: publishers (tailers, hook handlers, attention
// engine, quota ingest) call Hub.Publish; subscribers (SSE handlers, the
// attention engine) call Hub.Subscribe. The hub never blocks publishers —
// slow consumers drop events and increment a per-sub counter.
package events

import (
	"encoding/json"
	"time"
)

// Event is a single message routed through the hub. ID is assigned by
// the hub at Publish time if empty: "<unix-nano>-<seq>" where seq is a
// per-second monotonic counter so bursts within the same nanosecond
// remain unique and ordered.
type Event struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Session string          `json:"session,omitempty"`
	Payload json.RawMessage `json:"payload"`

	ts time.Time
}

// globalRing is the ring-buffer key used for events without a session.
const globalRing = ""
