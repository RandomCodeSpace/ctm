// Package store — quota_update → cost_points subscriber.
//
// Wired in server.go (coordinator-owned):
//
//	costDone := make(chan struct{})
//	go func() {
//	    defer close(costDone)
//	    store.SubscribeQuotaWriter(runCtx, hub, costDB)
//	}()
//
// No cancellation channel is returned; the goroutine exits when
// SubscribeQuotaWriter's ctx is cancelled (wired off the daemon's
// root ctx so shutdown draining matches the attention/webhook
// pattern in server.go).
package store

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/events"
)

// Per-million-token input/output/cache prices used to compute
// cost_usd_micros. Values track Claude Sonnet 4.5 public pricing
// (input $3/Mt, output $15/Mt, cache read $0.30/Mt). We store integers
// (USD * 1_000_000) rather than floats so SUM() stays exact across
// the seven-day window.
const (
	PriceInputPerMillionMicros  int64 = 3_000_000
	PriceOutputPerMillionMicros int64 = 15_000_000
	PriceCachePerMillionMicros  int64 = 300_000
)

// ComputeCostMicros returns USD * 1e6 for the given token triple.
// Exported for tests and so handlers never have to redo the math.
func ComputeCostMicros(input, output, cache int64) int64 {
	// micros = tokens * priceMicrosPerMillion / 1_000_000
	return (input*PriceInputPerMillionMicros +
		output*PriceOutputPerMillionMicros +
		cache*PriceCachePerMillionMicros) / 1_000_000
}

// quotaUpdatePayload matches the JSON that QuotaIngester.publishSession
// writes. Only per-session payloads (Session != "") carry token fields;
// global rate-limit updates are ignored by the writer.
type quotaUpdatePayload struct {
	Session      string `json:"session"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	CacheTokens  int64  `json:"cache_tokens"`
}

// SubscribeQuotaWriter subscribes to the hub's quota_update stream and
// persists each per-session update as a cost_points row. Blocks until
// ctx is cancelled or the subscription is closed by the hub.
//
// If ready is non-nil, it is closed as soon as the hub subscription is
// registered — callers that need to race-free publish between the
// "start this goroutine" and "publish the first event" lines should
// wait on ready first. Nil is fine for production, where events
// arrive asynchronously from the quota ingester well after startup.
//
// Write errors are logged and swallowed — a failed persistence must
// not take down the daemon, and the next update will carry the same
// cumulative token counts so no data is lost permanently.
func SubscribeQuotaWriter(ctx context.Context, hub *events.Hub, store CostStore, ready chan<- struct{}) {
	if hub == nil || store == nil {
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
			if ev.Type != "quota_update" {
				continue
			}
			if ev.Session == "" {
				// Global rate-limit update — no token counts to persist.
				continue
			}
			var p quotaUpdatePayload
			if err := json.Unmarshal(ev.Payload, &p); err != nil {
				slog.Debug("cost store: bad quota_update payload", "err", err)
				continue
			}
			// Guard against obviously-empty payloads. If all three token
			// counters are zero there's nothing useful to chart and
			// writing it just adds noise to Totals().
			if p.InputTokens == 0 && p.OutputTokens == 0 && p.CacheTokens == 0 {
				continue
			}
			cost := ComputeCostMicros(p.InputTokens, p.OutputTokens, p.CacheTokens)
			if err := store.Insert([]Point{{
				TS:            time.Now().UTC(),
				Session:       p.Session,
				InputTokens:   p.InputTokens,
				OutputTokens:  p.OutputTokens,
				CacheTokens:   p.CacheTokens,
				CostUSDMicros: cost,
			}}); err != nil {
				slog.Warn("cost store insert failed", "session", p.Session, "err", err)
			}
		}
	}
}
