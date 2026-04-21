// Package api implements the JSON HTTP handlers mounted under /api by
// internal/serve.Server. Handlers are pure http.HandlerFunc factories
// — auth wrapping happens at server boot, not inside the handler.
package api

import (
	"encoding/json"
	"net/http"
)

// Bootstrap returns the GET /api/bootstrap handler. The response shape
// is the contract consumed by ui/ on the auth-paste screen: enough
// metadata for the SPA to decide whether to render webhook UI without
// leaking server internals.
//
// Response: {"version":..., "port":..., "has_webhook":...}.
//
// 405 on any non-GET method.
func Bootstrap(version string, port int, hasWebhook bool) http.HandlerFunc {
	body := struct {
		Version    string `json:"version"`
		Port       int    `json:"port"`
		HasWebhook bool   `json:"has_webhook"`
	}{
		Version:    version,
		Port:       port,
		HasWebhook: hasWebhook,
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		// inputs are primitives — this is unreachable in practice
		panic("api.Bootstrap: marshal: " + err.Error())
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(encoded)
	}
}
