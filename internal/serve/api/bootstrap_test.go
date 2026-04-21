package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBootstrap_ResponseShape(t *testing.T) {
	h := Bootstrap("v1.2.3", 37778, true)
	req := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}

	body, _ := io.ReadAll(rec.Body)
	var got struct {
		Version    string `json:"version"`
		Port       int    `json:"port"`
		HasWebhook bool   `json:"has_webhook"`
		Extra      any    `json:"extra,omitempty"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v (body=%s)", err, body)
	}
	if got.Version != "v1.2.3" || got.Port != 37778 || got.HasWebhook != true {
		t.Errorf("body = %+v", got)
	}

	// Round-trip the keys to catch accidental renames.
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"version", "port", "has_webhook"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing key %q in response: %s", key, body)
		}
	}
	if len(raw) != 3 {
		t.Errorf("unexpected extra keys: %v", raw)
	}
}

func TestBootstrap_HasWebhookFalse(t *testing.T) {
	h := Bootstrap("dev", 1, false)
	req := httptest.NewRequest(http.MethodGet, "/api/bootstrap", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var got struct {
		HasWebhook bool `json:"has_webhook"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.HasWebhook {
		t.Errorf("has_webhook = true, want false")
	}
}

func TestBootstrap_MethodNotAllowed(t *testing.T) {
	h := Bootstrap("dev", 37778, false)
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		t.Run(m, func(t *testing.T) {
			req := httptest.NewRequest(m, "/api/bootstrap", nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("status = %d, want 405", rec.Code)
			}
			if got := rec.Header().Get("Allow"); got != http.MethodGet {
				t.Errorf("Allow = %q, want GET", got)
			}
		})
	}
}
