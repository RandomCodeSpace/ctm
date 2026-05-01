package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeHubStats struct{ payload any }

func (f fakeHubStats) Stats() any { return f.payload }

func TestHealthz_HappyPath(t *testing.T) {
	const hdr = "X-Ctm-Serve"
	const ver = "0.3.7"
	started := time.Now().Add(-2 * time.Second)
	h := Healthz(ver, hdr, started)

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get(hdr); got != ver {
		t.Errorf("%s header = %q, want %q", hdr, got, ver)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}

	var body struct {
		Status        string  `json:"status"`
		UptimeSeconds float64 `json:"uptime_seconds"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
	if body.UptimeSeconds < 1.5 {
		t.Errorf("uptime = %.2fs, want at least ~2s", body.UptimeSeconds)
	}
}

func TestHealthz_HEADReturnsHeadersWithoutBody(t *testing.T) {
	const hdr = "X-Ctm-Serve"
	h := Healthz("0.3.7", hdr, time.Now())
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodHead, "/healthz", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if rec.Header().Get(hdr) == "" {
		t.Errorf("expected header %q to be set on HEAD", hdr)
	}
}

func TestHealthz_MethodNotAllowed(t *testing.T) {
	h := Healthz("0.3.7", "X-Ctm-Serve", time.Now())
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		h(rec, httptest.NewRequest(m, "/healthz", nil))
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s status = %d, want 405", m, rec.Code)
		}
	}
}

func TestHealth_HappyPathWithHubStats(t *testing.T) {
	const hdr = "X-Ctm-Serve"
	const ver = "0.3.7"
	started := time.Now().Add(-1 * time.Second)
	stats := fakeHubStats{payload: map[string]any{"published": float64(42), "dropped": float64(0)}}
	h := Health(ver, hdr, started, stats)

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get(hdr); got != ver {
		t.Errorf("version header = %q, want %q", got, ver)
	}

	var body struct {
		Status        string            `json:"status"`
		Version       string            `json:"version"`
		UptimeSeconds float64           `json:"uptime_seconds"`
		Components    map[string]string `json:"components"`
		Hub           map[string]any    `json:"hub"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Status != "ok" || body.Version != ver {
		t.Errorf("status/version = (%q,%q), want (ok,%q)", body.Status, body.Version, ver)
	}
	if got := body.Components["http"]; got != "ok" {
		t.Errorf("components[http] = %q, want ok", got)
	}
	if got, _ := body.Hub["published"].(float64); got != 42 {
		t.Errorf("hub.published = %v, want 42", body.Hub["published"])
	}
}

func TestHealth_NilHubOmitsHubField(t *testing.T) {
	h := Health("0.3.7", "X-Ctm-Serve", time.Now(), nil)
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var body map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, present := body["hub"]; present {
		t.Errorf("expected 'hub' to be omitted when nil, got %v", body["hub"])
	}
}

func TestHealth_MethodNotAllowed(t *testing.T) {
	h := Health("0.3.7", "X-Ctm-Serve", time.Now(), nil)
	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/health", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}
