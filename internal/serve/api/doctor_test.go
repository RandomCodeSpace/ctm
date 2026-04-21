package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/doctor"
	"github.com/RandomCodeSpace/ctm/internal/serve/auth"
)

func TestDoctor_ResponseShape(t *testing.T) {
	stub := func(_ context.Context) []doctor.Check {
		return []doctor.Check{
			{Name: "dep:tmux", Status: doctor.StatusOK, Message: "/usr/bin/tmux"},
			{Name: "env:PATH", Status: doctor.StatusWarn, Message: "short", Remediation: "set PATH"},
			{Name: "serve:token", Status: doctor.StatusErr, Message: "missing", Remediation: "run ctm doctor"},
		}
	}
	h := Doctor(stub)
	req := httptest.NewRequest(http.MethodGet, "/api/doctor", nil)
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

	var got struct {
		Checks []doctor.Check `json:"checks"`
	}
	body, _ := io.ReadAll(rec.Body)
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v (body=%s)", err, body)
	}
	if len(got.Checks) != 3 {
		t.Fatalf("len(checks) = %d, want 3 (body=%s)", len(got.Checks), body)
	}
	if got.Checks[0].Status != doctor.StatusOK ||
		got.Checks[1].Status != doctor.StatusWarn ||
		got.Checks[2].Status != doctor.StatusErr {
		t.Errorf("status ordering mismatch: %+v", got.Checks)
	}

	// Remediation should only appear on rows that set it.
	var raw struct {
		Checks []map[string]any `json:"checks"`
	}
	_ = json.Unmarshal(body, &raw)
	if _, ok := raw.Checks[0]["remediation"]; ok {
		t.Errorf("remediation should be omitted on ok row: %s", body)
	}
	if raw.Checks[1]["remediation"] != "set PATH" {
		t.Errorf("remediation wrong on warn row: %v", raw.Checks[1])
	}
}

func TestDoctor_NilChecksBecomesEmptyArray(t *testing.T) {
	h := Doctor(func(_ context.Context) []doctor.Check { return nil })
	req := httptest.NewRequest(http.MethodGet, "/api/doctor", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	body, _ := io.ReadAll(rec.Body)
	// Must be {"checks":[]} not {"checks":null} — UI relies on this.
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	arr, ok := raw["checks"].([]any)
	if !ok {
		t.Fatalf("checks is not an array: %s", body)
	}
	if len(arr) != 0 {
		t.Errorf("want empty array, got %v", arr)
	}
}

func TestDoctor_MethodNotAllowed(t *testing.T) {
	h := Doctor(func(_ context.Context) []doctor.Check { return nil })
	for _, m := range []string{http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch} {
		req := httptest.NewRequest(m, "/api/doctor", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("%s: status = %d, want 405", m, rec.Code)
		}
		if got := rec.Header().Get("Allow"); got != http.MethodGet {
			t.Errorf("%s: Allow = %q, want GET", m, got)
		}
	}
}

// TestDoctor_AuthWrapped exercises the server-level auth requirement:
// the handler itself doesn't check auth (that's the server's job), but
// when mounted behind auth.Required the combined stack must reject
// unauthenticated requests before reaching the runner.
func TestDoctor_AuthWrapped(t *testing.T) {
	var called bool
	run := func(_ context.Context) []doctor.Check {
		called = true
		return nil
	}
	h := auth.Required("secret-token", Doctor(run))

	// No Authorization header.
	req := httptest.NewRequest(http.MethodGet, "/api/doctor", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("no-auth: status = %d, want 401", rec.Code)
	}
	if called {
		t.Error("runner was called despite missing auth")
	}

	// Correct bearer.
	req = httptest.NewRequest(http.MethodGet, "/api/doctor", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("authed: status = %d, want 200", rec.Code)
	}
	if !called {
		t.Error("runner was not called despite valid auth")
	}
}
