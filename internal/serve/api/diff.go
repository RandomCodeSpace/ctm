package api

import (
	"log/slog"
	"net/http"

	"github.com/RandomCodeSpace/ctm/internal/serve/git"
)

// diffFn is the seam tests inject; production code wires git.DiffAt.
// Kept package-private — callers wire through Diff().
var diffFn = git.DiffAt

// Diff returns the GET handler for
// /api/sessions/{name}/checkpoints/{sha}/diff.
//
// The handler chains two guards that together prevent arbitrary
// `git show` exposure:
//
//  1. resolveWorkdir maps a session name to its workdir (false → 404).
//  2. cache.IsCheckpoint confirms sha is one of the *full* commit
//     SHAs currently listed under the session's checkpoints. A SHA
//     that doesn't appear — including abbreviated forms — yields 404.
//
// On success the unified diff is streamed as text/plain so the UI can
// render it in a <pre> without JSON envelope overhead.
func Diff(resolveWorkdir func(name string) (string, bool), cache *CheckpointsCache) http.HandlerFunc {
	if cache == nil {
		cache = NewCheckpointsCache()
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		name := r.PathValue("name")
		sha := r.PathValue("sha")
		if name == "" {
			http.NotFound(w, r)
			return
		}
		// Cheap-first: reject obviously malformed SHA before doing any
		// lookup work. `git show` accepts abbreviated SHAs but the
		// checkpoint allowlist rejects them, so in practice every
		// legitimate caller sends a 40-char hex string.
		if !isFullSHA(sha) {
			http.Error(w, "invalid_sha", http.StatusBadRequest)
			return
		}

		workdir, ok := resolveWorkdir(name)
		if !ok {
			http.NotFound(w, r)
			return
		}

		// Full-SHA allowlist — same cache the /checkpoints handler and
		// the revert handler consult, so a cached list produced in the
		// last 5 s covers this call too.
		if !cache.IsCheckpoint(workdir, name, sha) {
			http.NotFound(w, r)
			return
		}

		out, err := diffFn(workdir, sha)
		if err != nil {
			slog.Error("checkpoint diff failed",
				"session", name,
				"sha", sha,
				"err", err)
			http.Error(w, "git_failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte(out))
	}
}

// isFullSHA reports whether s is exactly 40 lowercase-hex characters —
// the canonical git SHA-1 form. Any shorter or mixed-case input is
// rejected outright to keep the allowlist's blast radius minimal.
func isFullSHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}
