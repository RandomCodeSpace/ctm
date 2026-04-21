package api

// V23 — origin allowlist helper. See docs/v02/V23-mutation-auth.md.
//
// Mutation endpoints (kill / forget / rename / attach-deeplink) layer
// an Origin-header check on top of the existing bearer-token auth.
// Even though the daemon binds 127.0.0.1 and browsers require
// explicit CORS opt-in, a cross-origin POST from a tab the user has
// open to a hostile page would otherwise succeed by piggy-backing on
// the token stored in localStorage. Requiring Origin to match one of
// the known loopback URLs defeats that vector at zero cost to
// legitimate callers.

import (
	"net/http"
	"strings"
)

// DefaultAllowedOrigins is the baseline loopback allowlist. Future
// work may load a user-configurable extra list from
// config.Serve.AllowedOrigins; for v0.2 we hard-code the two loopback
// spellings the UI actually produces.
func DefaultAllowedOrigins(port int) []string {
	return []string{
		originFor("http://127.0.0.1", port),
		originFor("http://localhost", port),
	}
}

// originFor formats a scheme+host+port origin tuple without a
// trailing slash — matching the exact shape browsers send in the
// Origin request header (RFC 6454 §6.2).
func originFor(schemeHost string, port int) string {
	if port == 80 && strings.HasPrefix(schemeHost, "http://") {
		return schemeHost
	}
	if port == 443 && strings.HasPrefix(schemeHost, "https://") {
		return schemeHost
	}
	return schemeHost + ":" + itoa(port)
}

// itoa avoids importing strconv into this tiny file. Port is always
// a positive int; negative/zero should never reach us.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [10]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// RequireOrigin wraps h with a check that the Origin request header
// matches one of allowed. An empty Origin is rejected — same-origin
// fetches set it reliably in modern browsers, and non-browser callers
// (curl, ctm CLI) can set it explicitly. Missing/mismatched → 403.
func RequireOrigin(allowed []string, h http.Handler) http.Handler {
	set := make(map[string]struct{}, len(allowed))
	for _, o := range allowed {
		set[o] = struct{}{}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin == "" {
			http.Error(w, "missing Origin", http.StatusForbidden)
			return
		}
		if _, ok := set[origin]; !ok {
			http.Error(w, "disallowed Origin", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// RequireOriginFunc is the HandlerFunc-flavoured variant — convenient
// when wrapping the return of another HandlerFunc inline.
func RequireOriginFunc(allowed []string, h http.HandlerFunc) http.HandlerFunc {
	wrapped := RequireOrigin(allowed, h)
	return func(w http.ResponseWriter, r *http.Request) {
		wrapped.ServeHTTP(w, r)
	}
}
