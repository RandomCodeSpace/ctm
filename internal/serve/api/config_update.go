package api

// Coordinator wiring (do NOT edit server.go here — this comment is the
// integration contract). In server.go's registerRoutes, after the
// existing mux.Handle lines, add:
//
//   mux.Handle("GET /api/config", authHF(api.ConfigGet(config.ConfigPath())))
//   mux.Handle("PATCH /api/config", authHF(api.ConfigUpdate(config.ConfigPath(), s.Shutdown)))
//
// s.Shutdown(reason string) must cancel the daemon's root context so
// Run(ctx) returns cleanly; callers (ctm attach / new / yolo) then
// respawn via proc.EnsureServeRunning on the next user action.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/config"
)

// Validation bounds for the six attention thresholds. Keep these in
// sync with config.AttentionThresholds doc.
const (
	maxPct     = 100
	maxMinutes = 1440
)

// shutdownDelay is how long we wait after responding 202 before
// cancelling the daemon's root context. A small delay lets the HTTP
// response body flush to the client; otherwise the SPA would race the
// shutdown and see a truncated connection before it can show the
// "daemon restarting" banner.
const shutdownDelay = 1 * time.Second

// ConfigUpdate returns the PATCH /api/config handler.
//
// Body shape (all top-level keys optional; unknown keys → 400):
//
//	{
//	  "webhook_url":  "https://...",
//	  "webhook_auth": "Bearer ...",
//	  "attention": {
//	    "error_rate_pct":         20,
//	    "error_rate_window":      20,
//	    "idle_minutes":           5,
//	    "quota_pct":              85,
//	    "context_pct":            90,
//	    "yolo_unchecked_minutes": 30
//	  }
//	}
//
// On success, responds 202 Accepted with {"status":"restarting"} and
// schedules shutdown(reason) to run after shutdownDelay so the response
// flushes first. The caller (proc.EnsureServeRunning at the next
// attach/new/yolo) respawns the daemon.
//
// Contract:
//   - 405 on non-PATCH.
//   - 400 on invalid JSON, unknown top-level keys, or out-of-range values.
//   - 500 if the on-disk config cannot be loaded or atomically replaced.
//   - 202 with {"status":"restarting"} on success.
//
// Write is atomic: marshal → WriteFile(<path>.tmp) → Rename. A crash
// mid-write leaves the previous config intact.
func ConfigUpdate(cfgPath string, shutdown func(reason string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			w.Header().Set("Allow", http.MethodPatch)
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Decode with DisallowUnknownFields so typos like "webhookUrl"
		// or dropped experimental keys surface as 400 instead of being
		// silently ignored. The allowlist is the wire contract.
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		var req configPayload
		if err := dec.Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, sanitizeDecodeErr(err))
			return
		}

		// Range-check thresholds before touching disk.
		if req.Attention != nil {
			if msg, ok := validateAttention(req.Attention); !ok {
				writeJSONError(w, http.StatusBadRequest, msg)
				return
			}
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "load_config")
			return
		}

		// Deep-merge: only fields present in the patch body overwrite
		// existing config state. String fields are overwritten
		// verbatim (including empty strings, which lets users clear
		// webhook_url); attention thresholds merge field-by-field so a
		// partial attention block preserves the untouched thresholds.
		cfg.Serve.WebhookURL = req.WebhookURL
		cfg.Serve.WebhookAuth = req.WebhookAuth
		if req.Attention != nil {
			cfg.Serve.Attention = config.AttentionThresholds{
				ErrorRatePct:         req.Attention.ErrorRatePct,
				ErrorRateWindow:      req.Attention.ErrorRateWindow,
				IdleMinutes:          req.Attention.IdleMinutes,
				QuotaPct:             req.Attention.QuotaPct,
				ContextPct:           req.Attention.ContextPct,
				YoloUncheckedMinutes: req.Attention.YoloUncheckedMinutes,
			}
		}

		if err := writeAtomic(cfgPath, cfg); err != nil {
			writeJSONError(w, http.StatusInternalServerError, "write_config")
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "restarting"})

		// Flush before cancelling: the response goroutine must return
		// so http.Server can write the body out before BaseContext is
		// cancelled. A tiny delay plus a background goroutine avoids
		// coupling the restart to the response writer's lifetime.
		if shutdown != nil {
			go func() {
				time.Sleep(shutdownDelay)
				shutdown("config change")
			}()
		}
	}
}

// validateAttention enforces the documented ranges: percentages in
// (0, 100], minute windows in (0, 1440]. Zero is rejected because the
// config-layer Resolved() treats 0 as "use default", and the UI should
// not be able to implicitly reset a threshold by POSTing 0 — it must
// send the default value explicitly.
func validateAttention(a *attentionPayload) (string, bool) {
	checks := []struct {
		name string
		val  int
		max  int
	}{
		{"error_rate_pct", a.ErrorRatePct, maxPct},
		{"quota_pct", a.QuotaPct, maxPct},
		{"context_pct", a.ContextPct, maxPct},
		{"error_rate_window", a.ErrorRateWindow, maxMinutes},
		{"idle_minutes", a.IdleMinutes, maxMinutes},
		{"yolo_unchecked_minutes", a.YoloUncheckedMinutes, maxMinutes},
	}
	for _, c := range checks {
		if c.val <= 0 {
			return fmt.Sprintf("%s must be > 0", c.name), false
		}
		if c.val > c.max {
			return fmt.Sprintf("%s must be <= %d", c.name, c.max), false
		}
	}
	return "", true
}

// writeAtomic marshals cfg to JSON and swaps it into place via a
// sibling .tmp file + rename. The rename is atomic on POSIX when src
// and dst live on the same filesystem (always true here — both in
// ~/.config/ctm/).
func writeAtomic(path string, cfg config.Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	// Stamp the current schema version so the file round-trips cleanly
	// through the migrator on subsequent loads. Mirrors config.write().
	cfg.SchemaVersion = config.SchemaVersion
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// sanitizeDecodeErr maps json.Decoder errors to short, stable tokens.
// Unknown-field errors are the load-bearing 400 signal so the UI can
// render a helpful message; everything else collapses to
// "invalid_request" to avoid leaking parser internals.
func sanitizeDecodeErr(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	const marker = "json: unknown field "
	if idx := indexOf(msg, marker); idx >= 0 {
		// e.g. `json: unknown field "foo"` → `unknown key: foo`
		return "unknown key: " + trimQuotes(msg[idx+len(marker):])
	}
	return "invalid_request"
}

// indexOf is a tiny strings.Index shim to keep the import list lean
// (one-file handler — we don't need strings elsewhere here).
func indexOf(s, substr string) int {
	n := len(substr)
	for i := 0; i+n <= len(s); i++ {
		if s[i:i+n] == substr {
			return i
		}
	}
	return -1
}

// trimQuotes strips leading/trailing ASCII double-quotes from s.
// json's "unknown field" error format is `"field"` with the quotes
// embedded in the message, so we peel them for a cleaner wire token.
func trimQuotes(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
