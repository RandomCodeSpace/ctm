package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/RandomCodeSpace/ctm/internal/serve/git"
)

// revertFn is the seam tests inject; production wires git.Revert.
var revertFn = git.Revert

type revertRequest struct {
	SHA        string `json:"sha"`
	StashFirst bool   `json:"stash_first"`
}

// Revert returns the POST handler for /api/sessions/{name}/revert.
//
// resolveWorkdir maps a session name to its workdir (false → 404).
// allowedSHA reports whether `sha` appears in the corresponding
// `/checkpoints` listing for `name` — the sole guard preventing a
// caller from `git reset --hard` to an arbitrary SHA.
func Revert(
	resolveWorkdir func(name string) (string, bool),
	allowedSHA func(name, sha string) bool,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		name := r.PathValue("name")
		if name == "" {
			http.NotFound(w, r)
			return
		}

		workdir, ok := resolveWorkdir(name)
		if !ok {
			http.NotFound(w, r)
			return
		}

		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		var req revertRequest
		if err := dec.Decode(&req); err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid_request")
			return
		}
		req.SHA = strings.TrimSpace(req.SHA)
		if req.SHA == "" {
			writeJSONError(w, http.StatusBadRequest, "missing_sha")
			return
		}

		if !allowedSHA(name, req.SHA) {
			writeJSONError(w, http.StatusUnprocessableEntity, "sha_not_a_checkpoint")
			return
		}

		result, err := revertFn(workdir, req.SHA, req.StashFirst)
		if err != nil {
			var dirty *git.DirtyError
			if errors.As(err, &dirty) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Cache-Control", "no-store")
				w.WriteHeader(http.StatusConflict)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error":       "dirty_workdir",
					"dirty_files": dirty.Files,
				})
				return
			}
			// If the stash succeeded but the subsequent reset failed,
			// `result.StashedAs` carries the stash SHA the user will
			// need to recover with `git stash pop <sha>`. Surface it
			// so the UI can show a recovery hint instead of orphaning
			// the user's uncommitted work.
			body := map[string]any{"error": sanitiseErr(err)}
			if result.StashedAs != "" {
				body["stashed_as"] = result.StashedAs
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(body)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(result)
	}
}

func writeJSONError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// sanitiseErr collapses an error to a short, log-style token so we
// don't leak filesystem paths or git internals over the wire.
func sanitiseErr(err error) string {
	s := err.Error()
	// Keep the leading "verb" (e.g. "git reset --hard <sha>") and drop
	// anything past the first colon — that's where stderr starts.
	if i := strings.Index(s, ":"); i > 0 {
		return s[:i]
	}
	return s
}
