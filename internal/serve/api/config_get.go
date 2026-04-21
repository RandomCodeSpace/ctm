package api

// Coordinator wiring (do NOT edit server.go here — this comment is the
// integration contract). In server.go's registerRoutes, after the
// existing mux.Handle lines, add:
//
//   mux.Handle("GET /api/config", authHF(api.ConfigGet(config.ConfigPath())))
//   mux.Handle("PATCH /api/config", authHF(api.ConfigUpdate(config.ConfigPath(), s.Shutdown)))
//
// The second line assumes a Server.Shutdown(reason string) exists that
// cancels the daemon's root context. If it does not yet, wrap the
// Run(ctx) caller's cancel() via a thin closure at the call site.

import (
	"encoding/json"
	"net/http"

	"github.com/RandomCodeSpace/ctm/internal/config"
)

// ConfigGet returns the GET /api/config handler. The response is the
// same allowlisted shape that PATCH /api/config accepts, so the UI can
// seed the settings form with current values without parsing the full
// on-disk config.
//
// Response body:
//
//	{
//	  "webhook_url":  "...",
//	  "webhook_auth": "...",
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
// Thresholds are always returned in their Resolved() form so the UI
// form shows the defaults the daemon is actually using rather than
// zeroes for fields the user has never set.
//
// 405 on any non-GET method. 500 on config-load failure (rare — Load
// auto-creates the file on first boot).
func ConfigGet(cfgPath string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		cfg, err := config.Load(cfgPath)
		if err != nil {
			writeJSONError(w, http.StatusInternalServerError, "load_config")
			return
		}

		att := cfg.Serve.Attention.Resolved()
		body := configPayload{
			WebhookURL:  cfg.Serve.WebhookURL,
			WebhookAuth: cfg.Serve.WebhookAuth,
			Attention: &attentionPayload{
				ErrorRatePct:         att.ErrorRatePct,
				ErrorRateWindow:      att.ErrorRateWindow,
				IdleMinutes:          att.IdleMinutes,
				QuotaPct:             att.QuotaPct,
				ContextPct:           att.ContextPct,
				YoloUncheckedMinutes: att.YoloUncheckedMinutes,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(body)
	}
}

// configPayload is the wire shape for both GET and PATCH /api/config.
// On GET every field is populated; on PATCH clients may omit fields to
// leave them unchanged. `omitempty` on Attention lets partial patches
// skip the attention block entirely.
type configPayload struct {
	WebhookURL  string             `json:"webhook_url"`
	WebhookAuth string             `json:"webhook_auth"`
	Attention   *attentionPayload  `json:"attention,omitempty"`
}

type attentionPayload struct {
	ErrorRatePct         int `json:"error_rate_pct"`
	ErrorRateWindow      int `json:"error_rate_window"`
	IdleMinutes          int `json:"idle_minutes"`
	QuotaPct             int `json:"quota_pct"`
	ContextPct           int `json:"context_pct"`
	YoloUncheckedMinutes int `json:"yolo_unchecked_minutes"`
}
