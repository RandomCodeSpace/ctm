package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// QuotaSource is the subset of ingest.QuotaIngester the Quota handler
// depends on. Accepting an interface lets tests inject a fake without
// touching fsnotify.
type QuotaSource interface {
	// Snapshot returns WeeklyPct, FiveHourPct, WeeklyResetsAt,
	// FiveHourResetAt, Known — matching ingest.GlobalSnapshot field
	// order via a struct return.
	Snapshot() QuotaSnapshot
}

// QuotaSnapshot mirrors ingest.GlobalSnapshot so the api package
// doesn't need to import internal/serve/ingest (mirrors the pattern
// used by hubStatsAdapter in server.go). Exported because the
// quotaSourceAdapter in server.go converts between the ingest
// snapshot and this shape at the package boundary.
type QuotaSnapshot struct {
	WeeklyPct       int
	FiveHourPct     int
	WeeklyResetsAt  time.Time
	FiveHourResetAt time.Time
	Known           bool
}

// Quota returns the GET /api/quota handler. Shape mirrors the SSE
// `quota_update` global payload exactly so the SPA can feed the same
// TanStack cache key from both sources:
//
//	{"weekly_pct":46,"five_hr_pct":3,
//	 "weekly_resets_at":"2026-04-22T13:00:00Z",
//	 "five_hr_resets_at":"2026-04-21T18:00:00Z"}
//
// If no statusline dump has populated rate limits yet, responds 204
// No Content so the client leaves the cache null and renders "—"
// placeholders — matches spec §5 ("bars render before first event").
func Quota(src QuotaSource) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Cache-Control", "no-store")

		snap := src.Snapshot()
		if !snap.Known {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		body := struct {
			WeeklyPct      int    `json:"weekly_pct"`
			FiveHrPct      int    `json:"five_hr_pct"`
			WeeklyResetsAt string `json:"weekly_resets_at"`
			FiveHrResetsAt string `json:"five_hr_resets_at"`
		}{
			WeeklyPct:      snap.WeeklyPct,
			FiveHrPct:      snap.FiveHourPct,
			WeeklyResetsAt: rfc3339OrEmpty(snap.WeeklyResetsAt),
			FiveHrResetsAt: rfc3339OrEmpty(snap.FiveHourResetAt),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(body)
	}
}

func rfc3339OrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
