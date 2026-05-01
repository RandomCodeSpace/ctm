package api

// V23 — mutation endpoints: kill / forget / rename / attach-deeplink.
// Design rationale and threat model live in docs/v02/V23-mutation-auth.md
// (auth recipe A+B+D: bearer + type-to-confirm + Origin check).
//
// Wiring (central, server.go) — wrap each POST with
// api.RequireOriginFunc(allowed, …) in addition to authHF(…):
//
//   allowed := api.DefaultAllowedOrigins(s.opts.Port)
//   mux.Handle("POST /api/sessions/{name}/kill",
//       authHF(api.RequireOriginFunc(allowed, api.Kill(s.sessionStore, s.tmuxClient, s.proj))))
//   mux.Handle("POST /api/sessions/{name}/forget",
//       authHF(api.RequireOriginFunc(allowed, api.Forget(s.sessionStore, s.proj))))
//   mux.Handle("POST /api/sessions/{name}/rename",
//       authHF(api.RequireOriginFunc(allowed, api.Rename(s.sessionStore, s.tmuxClient, s.proj))))
//   mux.Handle("GET /api/sessions/{name}/attach-url",
//       authHF(api.RequireOriginFunc(allowed, api.AttachURL())))

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"

	"github.com/RandomCodeSpace/ctm/internal/session"
)

// SessionStore is the narrow slice of *session.Store the mutation
// handlers need. A package-local interface keeps the api package
// decoupled from the concrete store and makes the handlers trivially
// faked in tests.
type SessionStore interface {
	Get(name string) (*session.Session, error)
	Delete(name string) error
	Rename(oldName, newName string) error
}

// TmuxMutator is the narrow slice of *tmux.Client that kill / rename
// need. Mirrors the same decoupling pattern used by TmuxPaneCapturer.
type TmuxMutator interface {
	KillSession(name string) error
	RenameSession(oldName, newName string) error
}

// ProjRefresher triggers a projection reload after state mutations so
// /api/sessions reflects the new truth without waiting for the next
// polling tick.
type ProjRefresher interface {
	Reload()
}

// ----- kill -----------------------------------------------------------------

type killReq struct {
	Confirm string `json:"confirm"`
}

// Kill returns POST /api/sessions/{name}/kill. Body must be
// `{"confirm":"<session-name>"}` matching the path param (B in the
// design doc). Runs tmux kill-session and triggers a projection
// refresh. Returns 200 + the updated session JSON.
func Kill(store SessionStore, tmuxClient TmuxMutator, proj ProjRefresher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		name := r.PathValue("name")
		if name == "" {
			http.Error(w, errMsgMissingSessionName, http.StatusBadRequest)
			return
		}

		var body killReq
		if err := decodeJSON(r, &body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if body.Confirm != name {
			http.Error(w, "confirm must match session name", http.StatusBadRequest)
			return
		}

		sess, err := store.Get(name)
		if err != nil {
			http.Error(w, errMsgSessionNotFound, http.StatusNotFound)
			return
		}

		// tmux may already be down for the session — that's fine and
		// still counts as "killed". Surface other errors as 500 so the
		// UI doesn't silently lie about state.
		if err := tmuxClient.KillSession(name); err != nil && !isAlreadyGone(err) {
			http.Error(w, "tmux kill-session: "+err.Error(), http.StatusInternalServerError)
			return
		}

		proj.Reload()
		writeJSON(w, http.StatusOK, sess)
	}
}

// ----- forget ---------------------------------------------------------------

type forgetReq struct {
	Confirm string `json:"confirm"`
}

// Forget returns POST /api/sessions/{name}/forget. Removes the
// session from sessions.json but keeps the JSONL log so the user can
// still search history. Body must be `{"confirm":"<name>"}` (B).
func Forget(store SessionStore, proj ProjRefresher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		name := r.PathValue("name")
		if name == "" {
			http.Error(w, errMsgMissingSessionName, http.StatusBadRequest)
			return
		}

		var body forgetReq
		if err := decodeJSON(r, &body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if body.Confirm != name {
			http.Error(w, "confirm must match session name", http.StatusBadRequest)
			return
		}

		sess, err := store.Get(name)
		if err != nil {
			http.Error(w, errMsgSessionNotFound, http.StatusNotFound)
			return
		}
		if err := store.Delete(name); err != nil {
			http.Error(w, "sessions.json write: "+err.Error(), http.StatusInternalServerError)
			return
		}

		proj.Reload()
		writeJSON(w, http.StatusOK, sess)
	}
}

// ----- rename ---------------------------------------------------------------

type renameReq struct {
	To string `json:"to"`
}

// Rename returns POST /api/sessions/{name}/rename. Body:
// `{"to":"<new-name>"}`. Does not require type-to-confirm — rename
// is recoverable. Performs the tmux rename-session first so a name
// collision fails loudly before sessions.json gets touched.
func Rename(store SessionStore, tmuxClient TmuxMutator, proj ProjRefresher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			methodNotAllowed(w, http.MethodPost)
			return
		}
		name := r.PathValue("name")
		if name == "" {
			http.Error(w, errMsgMissingSessionName, http.StatusBadRequest)
			return
		}

		var body renameReq
		if err := decodeJSON(r, &body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if err := session.ValidateName(body.To); err != nil {
			http.Error(w, "invalid new name: "+err.Error(), http.StatusBadRequest)
			return
		}
		if body.To == name {
			http.Error(w, "new name matches current", http.StatusBadRequest)
			return
		}

		if _, err := store.Get(name); err != nil {
			http.Error(w, errMsgSessionNotFound, http.StatusNotFound)
			return
		}

		if err := tmuxClient.RenameSession(name, body.To); err != nil && !isAlreadyGone(err) {
			http.Error(w, "tmux rename-session: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if err := store.Rename(name, body.To); err != nil {
			http.Error(w, "sessions.json write: "+err.Error(), http.StatusInternalServerError)
			return
		}

		proj.Reload()
		// Re-fetch by new name so we return the post-rename session.
		sess, err := store.Get(body.To)
		if err != nil {
			http.Error(w, "post-rename lookup: "+err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, sess)
	}
}

// ----- attach-deeplink ------------------------------------------------------

// AttachURL returns GET /api/sessions/{name}/attach-url. Produces a
// `ctm://attach?name=…` URL the OS can hand off to the ctm CLI's
// attach handler. Read-only aside from formatting the URL, but still
// gated by Origin so a rogue page can't enumerate session names by
// probing this endpoint.
func AttachURL() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			methodNotAllowed(w, http.MethodGet)
			return
		}
		name := r.PathValue("name")
		if name == "" {
			http.Error(w, errMsgMissingSessionName, http.StatusBadRequest)
			return
		}
		q := url.Values{}
		q.Set("name", name)
		writeJSON(w, http.StatusOK, map[string]string{
			"url": "ctm://attach?" + q.Encode(),
		})
	}
}

// ----- shared helpers -------------------------------------------------------

func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errors.New("invalid JSON: " + err.Error())
	}
	return nil
}

// isAlreadyGone returns true when tmux reports that the target
// session doesn't exist. tmux's CLI prints "can't find session" in
// that case; os/exec wraps it in a *exec.ExitError whose Stderr we
// could parse, but the message is already surfaced via err.Error().
// We accept any error containing either literal since tmux versions
// differ slightly.
func isAlreadyGone(err error) bool {
	msg := err.Error()
	return contains(msg, "can't find session") ||
		contains(msg, "session not found") ||
		contains(msg, "no server running")
}

func contains(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
