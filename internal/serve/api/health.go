// Package api holds the v0.1 HTTP handlers for ctm serve. Only the
// liveness endpoints (/healthz, /health) live here in step 1 of the
// design spec; sessions, hooks, revert, and bootstrap arrive in
// subsequent steps.
package api

import (
	"encoding/json"
	"net/http"
	"time"
)

type healthzResponse struct {
	Status        string  `json:"status"`
	UptimeSeconds float64 `json:"uptime_seconds"`
}

type healthResponse struct {
	Status        string            `json:"status"`
	Version       string            `json:"version"`
	UptimeSeconds float64           `json:"uptime_seconds"`
	Components    map[string]string `json:"components"`
	Hub           any               `json:"hub,omitempty"`
}

// Healthz returns the unauthenticated liveness endpoint. The headerName
// (typically "X-Ctm-Serve") is set to version on every response so the
// single-instance guard can identify a sibling daemon portably without
// /proc/<pid>/cmdline.
func Healthz(version, headerName string, startedAt time.Time) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(headerName, version)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(healthzResponse{
			Status:        "ok",
			UptimeSeconds: time.Since(startedAt).Seconds(),
		})
	}
}

// HealthHubStats lets the daemon inject live hub statistics into the
// /health response without health.go importing the events package
// (cycle). server.go wires it via Health().
type HealthHubStats interface {
	Stats() any
}

// Health returns the rich, component-level health endpoint. Surfaces
// hub stats (subscriber count, publish/drop totals, ring sizes) so
// "is anyone subscribed to /events/all?" is observable from outside.
func Health(version, headerName string, startedAt time.Time, hub HealthHubStats) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set(headerName, version)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		var hubStats any
		if hub != nil {
			hubStats = hub.Stats()
		}
		_ = json.NewEncoder(w).Encode(healthResponse{
			Status:        "ok",
			Version:       version,
			UptimeSeconds: time.Since(startedAt).Seconds(),
			Components:    map[string]string{"http": "ok"},
			Hub:           hubStats,
		})
	}
}
