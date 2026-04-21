package auth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func okHandler(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*called = true
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func TestRequired_PanicsOnEmptyToken(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for empty token, got none")
		}
	}()
	_ = Required("", http.NotFoundHandler())
}

func TestRequired_TableDriven(t *testing.T) {
	const tok = "secret-token-abc"

	cases := []struct {
		name       string
		header     string
		wantStatus int
		wantNext   bool
	}{
		{name: "missing header", header: "", wantStatus: http.StatusUnauthorized, wantNext: false},
		{name: "wrong scheme", header: "Basic Zm9vOmJhcg==", wantStatus: http.StatusUnauthorized, wantNext: false},
		{name: "wrong token", header: "Bearer not-the-token", wantStatus: http.StatusUnauthorized, wantNext: false},
		{name: "empty bearer value", header: "Bearer ", wantStatus: http.StatusUnauthorized, wantNext: false},
		{name: "case-insensitive scheme", header: "bEaReR " + tok, wantStatus: http.StatusOK, wantNext: true},
		{name: "trims whitespace after scheme", header: "Bearer   " + tok + "  ", wantStatus: http.StatusOK, wantNext: true},
		{name: "happy path", header: "Bearer " + tok, wantStatus: http.StatusOK, wantNext: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			h := Required(tok, okHandler(&called))

			req := httptest.NewRequest(http.MethodGet, "/anything", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if called != tc.wantNext {
				t.Errorf("next invoked = %v, want %v", called, tc.wantNext)
			}
			if !tc.wantNext {
				// 401 invariants
				if got := rec.Header().Get("WWW-Authenticate"); got != `Bearer realm="ctm-serve"` {
					t.Errorf("WWW-Authenticate = %q", got)
				}
				if got := rec.Header().Get("Content-Type"); got != "application/json" {
					t.Errorf("Content-Type = %q", got)
				}
				if got := rec.Header().Get("Cache-Control"); got != "no-store" {
					t.Errorf("Cache-Control = %q", got)
				}
				body, _ := io.ReadAll(rec.Body)
				if !strings.Contains(string(body), `"unauthorized"`) {
					t.Errorf("body = %q", body)
				}
			}
		})
	}
}
