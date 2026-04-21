package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/doctor"
)

// DoctorRunner is the seam the handler uses to produce a []doctor.Check.
// Tests stub it to force arbitrary status rows; production injects a
// function that calls doctor.Run(ctx, cfg) with the daemon's Config.
type DoctorRunner func(ctx context.Context) []doctor.Check

// DoctorDeadline caps how long the runner may spend. The CLI doctor
// shells out to tmux and checks tmux session liveness; 5 s is ample
// for that without letting a pathological box hang the HTTP response.
const DoctorDeadline = 5 * time.Second

// Doctor returns the GET /api/doctor handler.
//
// Response shape (wire contract):
//
//	{
//	  "checks": [
//	    {"name": "dep:tmux", "status": "ok", "message": "/usr/bin/tmux"},
//	    {"name": "env:PATH", "status": "ok", "message": "set"},
//	    {"name": "serve:token", "status": "warn", "message": "...", "remediation": "..."}
//	  ]
//	}
//
// Auth wrapping happens at server boot (see server.go).
func Doctor(run DoctorRunner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), DoctorDeadline)
		defer cancel()

		checks := run(ctx)
		if checks == nil {
			// Always emit an array so the UI can render an empty state
			// without tripping over a JSON null.
			checks = []doctor.Check{}
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(struct {
			Checks []doctor.Check `json:"checks"`
		}{Checks: checks})
	}
}

// DefaultDoctorRunner is the production adapter: calls doctor.Run with
// the live Config under ctx. Kept out of Doctor() so tests can inject
// a deterministic stub via DoctorRunner.
func DefaultDoctorRunner(cfg config.Config) DoctorRunner {
	return func(ctx context.Context) []doctor.Check {
		return doctor.Run(ctx, cfg)
	}
}
