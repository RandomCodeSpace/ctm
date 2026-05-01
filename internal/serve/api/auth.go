package api

// V27 — /api/auth/{status,signup,login,logout}. Spec:
// docs/superpowers/specs/2026-04-22-V27-single-user-auth-design.md

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"math"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/RandomCodeSpace/ctm/internal/serve/auth"
)

var authUsernameRe = regexp.MustCompile(`^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`)

const authUsernameMax = 254

const authPasswordMin = 8
const authBodyMax = 1024

const (
	authMsgPostOnly    = "POST only"
	authLogLoginReject = "auth login reject"
)

// HTTP header / value constants shared across handlers in this package.
const (
	headerContentType   = "Content-Type"
	headerCacheControl  = "Cache-Control"
	contentTypeJSON     = "application/json"
	cacheControlNoStore = "no-store"
)

type authCredsBody struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// AuthStatus returns GET /api/auth/status. Never 401s — reports
// registered+authenticated as booleans so the UI can route.
func AuthStatus(store *auth.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", http.MethodGet)
			writeInputErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "GET only")
			return
		}
		resp := struct {
			Registered    bool `json:"registered"`
			Authenticated bool `json:"authenticated"`
		}{
			Registered: auth.Exists(),
		}
		if tok := bearerToken(r); tok != "" {
			if _, ok := store.Lookup(tok); ok {
				resp.Authenticated = true
			}
		}
		w.Header().Set(headerContentType, contentTypeJSON)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

// AuthSignup returns POST /api/auth/signup. Refuses if user.json
// already exists; otherwise creates it, issues a session token,
// returns 201.
func AuthSignup(store *auth.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeInputErr(w, http.StatusMethodNotAllowed, "method_not_allowed", authMsgPostOnly)
			return
		}
		if auth.Exists() {
			slog.Info("auth signup reject", "reason", "already_registered")
			writeInputErr(w, http.StatusConflict, "already_registered",
				"this instance already has a user; log in instead")
			return
		}
		var body authCredsBody
		if err := decodeAuthBody(r, w, &body); err != nil {
			return
		}
		if err := validateCreds(body); err != nil {
			slog.Info("auth signup reject", "reason", err.Error())
			writeInputErr(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		enc, err := auth.Hash(body.Password)
		if err != nil {
			slog.Error("auth signup hash error", "err", err.Error())
			writeInputErr(w, http.StatusInternalServerError, "hash_failed", err.Error())
			return
		}
		if err := auth.Save(auth.User{Username: body.Username, Password: enc}); err != nil {
			slog.Error("auth signup save error", "err", err.Error())
			writeInputErr(w, http.StatusInternalServerError, "save_failed", err.Error())
			return
		}
		tok, err := store.Create(body.Username)
		if err != nil {
			writeInputErr(w, http.StatusInternalServerError, "session_failed", err.Error())
			return
		}
		slog.Info("auth signup ok", "username", body.Username)
		w.Header().Set(headerContentType, contentTypeJSON)
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"token":    tok,
			"username": body.Username,
		})
	}
}

// AuthLogin returns POST /api/auth/login. The limiter protects the
// argon2id verify path from brute-force/DoS; a successful login
// resets the IP's window so legitimate users aren't locked out
// after a typo.
func AuthLogin(store *auth.Store, limiter *auth.Limiter) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeInputErr(w, http.StatusMethodNotAllowed, "method_not_allowed", authMsgPostOnly)
			return
		}
		ip := clientIP(r)
		if ok, retryAfter := limiter.Allow(ip); !ok {
			secs := int(math.Ceil(retryAfter.Seconds()))
			if secs < 1 {
				secs = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(secs))
			slog.Info(authLogLoginReject, "reason", "rate_limited", "ip", ip)
			writeInputErr(w, http.StatusTooManyRequests, "rate_limited",
				"too many login attempts; try again later")
			return
		}
		var body authCredsBody
		if err := decodeAuthBody(r, w, &body); err != nil {
			return
		}
		u, err := auth.Load()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				slog.Info(authLogLoginReject, "reason", "not_registered")
				writeInputErr(w, http.StatusNotFound, "not_registered",
					"no user exists yet; sign up first")
				return
			}
			slog.Error("auth login load error", "err", err.Error())
			writeInputErr(w, http.StatusInternalServerError, "load_failed", err.Error())
			return
		}
		if u.Username != body.Username || !auth.Verify(u.Password, body.Password) {
			slog.Info(authLogLoginReject, "reason", "invalid_credentials", "attempted_username", body.Username)
			writeInputErr(w, http.StatusUnauthorized, "invalid_credentials",
				"username or password does not match")
			return
		}
		limiter.Reset(ip)
		tok, err := store.Create(u.Username)
		if err != nil {
			writeInputErr(w, http.StatusInternalServerError, "session_failed", err.Error())
			return
		}
		slog.Info("auth login ok", "username", u.Username)
		w.Header().Set(headerContentType, contentTypeJSON)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"token":    tok,
			"username": u.Username,
		})
	}
}

// clientIP returns the host portion of r.RemoteAddr. We deliberately
// do NOT honour X-Forwarded-For: behind the reverse proxy the real
// source IP should reach us via RemoteAddr, and trusting XFF blindly
// would let any client spoof the rate-limit key.
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// AuthLogout returns POST /api/auth/logout.
func AuthLogout(store *auth.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeInputErr(w, http.StatusMethodNotAllowed, "method_not_allowed", authMsgPostOnly)
			return
		}
		tok := bearerToken(r)
		if tok == "" {
			writeInputErr(w, http.StatusUnauthorized, "missing_token", "")
			return
		}
		user, ok := store.Lookup(tok)
		if !ok {
			writeInputErr(w, http.StatusUnauthorized, "invalid_token", "")
			return
		}
		store.Revoke(tok)
		slog.Info("auth logout", "username", user)
		w.WriteHeader(http.StatusNoContent)
	}
}

// ---------- helpers --------------------------------------------------------

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return ""
	}
	return strings.TrimSpace(h[len(prefix):])
}

// BearerFromRequest is the exported twin of bearerToken, used by
// internal/serve/server.go's authHF middleware.
func BearerFromRequest(r *http.Request) string { return bearerToken(r) }

func decodeAuthBody(r *http.Request, w http.ResponseWriter, out *authCredsBody) error {
	r.Body = http.MaxBytesReader(w, r.Body, authBodyMax)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(out); err != nil {
		writeInputErr(w, http.StatusBadRequest, "invalid_body", err.Error())
		return err
	}
	return nil
}

func validateCreds(b authCredsBody) error {
	if len(b.Username) > authUsernameMax || !authUsernameRe.MatchString(b.Username) {
		return errors.New("username must be a valid email address")
	}
	if len(b.Password) < authPasswordMin {
		return errors.New("password must be at least 8 characters")
	}
	if strings.TrimSpace(b.Password) == "" {
		return errors.New("password cannot be whitespace only")
	}
	return nil
}
