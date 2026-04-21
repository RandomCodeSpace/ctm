package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDefaultAllowedOrigins_BuildsLoopbackPair(t *testing.T) {
	got := DefaultAllowedOrigins(37778)
	want := []string{
		"http://127.0.0.1:37778",
		"http://localhost:37778",
	}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] %q want %q", i, got[i], want[i])
		}
	}
}

func TestOriginFor_OmitsDefaultPorts(t *testing.T) {
	if got := originFor("http://example.com", 80); got != "http://example.com" {
		t.Errorf("http:80 -> %q want http://example.com", got)
	}
	if got := originFor("https://example.com", 443); got != "https://example.com" {
		t.Errorf("https:443 -> %q want https://example.com", got)
	}
	if got := originFor("http://127.0.0.1", 37778); got != "http://127.0.0.1:37778" {
		t.Errorf("loopback -> %q", got)
	}
}

func TestRequireOrigin_AllowsMatchingOrigin(t *testing.T) {
	allowed := DefaultAllowedOrigins(37778)
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	h := RequireOrigin(allowed, inner)

	req := httptest.NewRequest(http.MethodPost, "/kill", nil)
	req.Header.Set("Origin", "http://127.0.0.1:37778")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("status=%d want 204", rr.Code)
	}
}

func TestRequireOrigin_RejectsMismatchedOrigin(t *testing.T) {
	allowed := DefaultAllowedOrigins(37778)
	h := RequireOrigin(allowed, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/kill", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status=%d want 403", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "disallowed") {
		t.Errorf("body=%q want 'disallowed'", rr.Body.String())
	}
}

func TestRequireOrigin_RejectsMissingOrigin(t *testing.T) {
	h := RequireOrigin(DefaultAllowedOrigins(37778), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	req := httptest.NewRequest(http.MethodPost, "/kill", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("status=%d want 403", rr.Code)
	}
}

func TestRequireOriginFunc_WrapsHandlerFunc(t *testing.T) {
	called := false
	wrapped := RequireOriginFunc(DefaultAllowedOrigins(37778), func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	})

	req := httptest.NewRequest(http.MethodPost, "/kill", nil)
	req.Header.Set("Origin", "http://localhost:37778")
	rr := httptest.NewRecorder()
	wrapped(rr, req)

	if rr.Code != http.StatusTeapot {
		t.Errorf("status=%d want 418", rr.Code)
	}
	if !called {
		t.Error("inner handler not invoked")
	}
}
