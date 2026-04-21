// Package api — V13 /api/cost handler.
//
// Returns a window of cost_points plus totals so the dashboard can
// render a cumulative cost chart that survives daemon restarts.
//
// Required server.go wiring (coordinator owns this):
//
//	mux.Handle("GET /api/cost", authHF(api.Cost(s.cost)))
//
// s.cost must satisfy api.CostSource (see below). The production
// implementation is store.CostStore — use a direct assignment; the
// interfaces match by duck-typing (Range + Totals).
package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// CostSource is the subset of store.CostStore the Cost handler depends
// on. Accepting an interface keeps this package decoupled from the
// store package (mirrors the QuotaSource / LogsUsage patterns) and
// lets tests swap in a fake without opening a SQLite file.
type CostSource interface {
	Range(session string, since, until time.Time) ([]CostPoint, error)
	Totals(since time.Time) (CostTotals, error)
}

// CostPoint mirrors store.Point in wire form. Exported so the
// server.go adapter can type-assert cleanly.
type CostPoint struct {
	TS            time.Time
	Session       string
	InputTokens   int64
	OutputTokens  int64
	CacheTokens   int64
	CostUSDMicros int64
}

// CostTotals mirrors store.Totals for the same decoupling reason.
type CostTotals struct {
	InputTokens   int64
	OutputTokens  int64
	CacheTokens   int64
	CostUSDMicros int64
}

// windowDurations maps the accepted ?window= values to a since-cutoff.
// Unknown values are rejected with 400 so the UI can't silently show
// an empty chart because of a typo.
var windowDurations = map[string]time.Duration{
	"hour": time.Hour,
	"day":  24 * time.Hour,
	"week": 7 * 24 * time.Hour,
}

type costPointJSON struct {
	TS            string `json:"ts"`
	Session       string `json:"session"`
	InputTokens   int64  `json:"input_tokens"`
	OutputTokens  int64  `json:"output_tokens"`
	CacheTokens   int64  `json:"cache_tokens"`
	CostUSDMicros int64  `json:"cost_usd_micros"`
}

type costTotalsJSON struct {
	Input         int64 `json:"input"`
	Output        int64 `json:"output"`
	Cache         int64 `json:"cache"`
	CostUSDMicros int64 `json:"cost_usd_micros"`
}

type costResponse struct {
	Window string          `json:"window"`
	Points []costPointJSON `json:"points"`
	Totals costTotalsJSON  `json:"totals"`
}

// Cost returns the GET /api/cost handler.
//
//	?session=<name>  — optional; omitted = aggregate across all sessions
//	?window=hour|day|week — default day; unknown = 400
func Cost(src CostSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")

		window := r.URL.Query().Get("window")
		if window == "" {
			window = "day"
		}
		dur, ok := windowDurations[window]
		if !ok {
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "unknown_window",
				"hint":  "window must be one of hour|day|week",
			})
			return
		}

		session := r.URL.Query().Get("session")

		now := time.Now().UTC()
		since := now.Add(-dur)

		points, err := src.Range(session, since, now)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "range_failed"})
			return
		}
		totals, err := src.Totals(since)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "totals_failed"})
			return
		}

		out := costResponse{
			Window: window,
			Points: make([]costPointJSON, 0, len(points)),
			Totals: costTotalsJSON{
				Input:         totals.InputTokens,
				Output:        totals.OutputTokens,
				Cache:         totals.CacheTokens,
				CostUSDMicros: totals.CostUSDMicros,
			},
		}
		for _, p := range points {
			out.Points = append(out.Points, costPointJSON{
				TS:            p.TS.UTC().Format(time.RFC3339Nano),
				Session:       p.Session,
				InputTokens:   p.InputTokens,
				OutputTokens:  p.OutputTokens,
				CacheTokens:   p.CacheTokens,
				CostUSDMicros: p.CostUSDMicros,
			})
		}
		_ = json.NewEncoder(w).Encode(out)
	}
}
