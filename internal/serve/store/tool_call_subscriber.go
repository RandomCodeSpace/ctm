package store

// V19 slice 3 — stream tool_call events from the hub into the FTS index.
//
// The tailer replays each session's JSONL from offset 0 on boot, so
// this subscriber indexes both historical and live rows without
// needing a separate backfill loop. OpenCostStore wipes the FTS table
// on each boot, which keeps dedup trivial: every restart rebuilds a
// clean index from the incoming event stream.
//
// Wired in server.go alongside the cost subscriber:
//
//	toolCallDone := make(chan struct{})
//	go func() {
//	    defer close(toolCallDone)
//	    store.SubscribeToolCallWriter(runCtx, hub, costDB, nil)
//	}()

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/events"
)

// ToolCallIndexer is satisfied by sqliteCostStore. Kept narrow so
// tests can swap in a spy.
type ToolCallIndexer interface {
	IndexToolCall(session, tool, content string, ts time.Time) error
}

// toolCallPayload mirrors ingest.ToolCallPayload; duplicated here to
// avoid a store → ingest dependency.
type toolCallPayload struct {
	Session string    `json:"session"`
	Tool    string    `json:"tool"`
	Input   string    `json:"input,omitempty"`
	Summary string    `json:"summary,omitempty"`
	IsError bool      `json:"is_error"`
	TS      time.Time `json:"ts"`
}

// SubscribeToolCallWriter subscribes to every tool_call event and
// writes a searchable row to the FTS index. Returns when ctx is
// cancelled or the subscription channel closes. `ready`, if non-nil,
// is closed once the subscription attaches — tests wait on it to
// avoid racing the hub.
func SubscribeToolCallWriter(
	ctx context.Context,
	hub *events.Hub,
	idx ToolCallIndexer,
	ready chan<- struct{},
) {
	if hub == nil || idx == nil {
		if ready != nil {
			close(ready)
		}
		return
	}
	sub, _ := hub.Subscribe("", "")
	defer sub.Close()
	if ready != nil {
		close(ready)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.Events():
			if !ok {
				return
			}
			if ev.Type != "tool_call" {
				continue
			}
			var p toolCallPayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				slog.Debug("fts subscriber: malformed payload", "err", err)
				continue
			}
			content := joinNonEmpty(p.Input, p.Summary)
			if content == "" {
				continue
			}
			session := p.Session
			if session == "" {
				session = ev.Session
			}
			ts := p.TS
			if ts.IsZero() {
				ts = time.Now().UTC()
			}
			if err := idx.IndexToolCall(session, p.Tool, content, ts); err != nil {
				slog.Warn("fts subscriber: index write failed",
					"session", session, "tool", p.Tool, "err", err)
			}
		}
	}
}

func joinNonEmpty(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(p)
	}
	return b.String()
}
