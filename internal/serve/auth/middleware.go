package auth

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
)

// bearerPrefix is matched case-insensitively per RFC 7235 §2.1.
const bearerPrefix = "bearer "

// Required returns an HTTP middleware that enforces a static bearer
// token. Requests without an `Authorization: Bearer <token>` header
// matching the expected token (constant-time compared) are rejected
// with 401 + a small JSON body and the standard `WWW-Authenticate`
// challenge header.
//
// An empty `token` argument is a programming error (Required is wired
// at server boot, not per-request) and panics rather than silently
// disabling auth.
func Required(token string, next http.Handler) http.Handler {
	if token == "" {
		panic("auth.Required: empty token (auth would be disabled)")
	}
	expected := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		if header == "" {
			slog.Warn("auth deny: no Authorization header",
				"path", r.URL.Path, "remote", r.RemoteAddr,
				"origin", r.Header.Get("Origin"), "referer", r.Header.Get("Referer"))
			deny(w)
			return
		}
		if len(header) < len(bearerPrefix) || !strings.EqualFold(header[:len(bearerPrefix)], bearerPrefix) {
			slog.Warn("auth deny: malformed Authorization scheme",
				"path", r.URL.Path, "scheme_prefix", safePrefix(header, 8))
			deny(w)
			return
		}
		got := strings.TrimSpace(header[len(bearerPrefix):])
		if subtle.ConstantTimeCompare([]byte(got), expected) != 1 {
			slog.Warn("auth deny: token mismatch",
				"path", r.URL.Path,
				"got_len", len(got), "want_len", len(expected),
				"got_fp", fingerprint(got), "want_fp", fingerprint(string(expected)))
			deny(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// fingerprint returns the first 4 + last 4 chars of a token joined by "…".
// Safe to log: with 43 url-safe-b64 chars (~256 bits of entropy), exposing 8
// chars reveals <50 bits and still lets us compare "is this the same token"
// across log lines / between localStorage and disk.
func fingerprint(s string) string {
	if len(s) <= 9 {
		return "(short)"
	}
	return s[:4] + "…" + s[len(s)-4:]
}

// safePrefix returns up to n chars of s; used to log the scheme portion of
// a malformed Authorization header without risking secret exposure.
func safePrefix(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}

func deny(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("WWW-Authenticate", `Bearer realm="ctm-serve"`)
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
}
