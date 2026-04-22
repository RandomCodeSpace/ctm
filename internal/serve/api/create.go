package api

// V26 — POST /api/sessions. Creates a detached yolo-mode claude
// session. Spec: docs/superpowers/specs/2026-04-22-V26-create-session-design.md

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/RandomCodeSpace/ctm/internal/session"
)

// CreateSpawner is the thin seam into session.Yolo. Keeping it
// behind an interface lets create_test.go exercise the handler
// without spawning real tmux + claude.
type CreateSpawner interface {
	Spawn(name, workdir string) (session.Session, error)
	// SendInitialPrompt fires a one-shot prompt into the new session
	// after claude has had time to boot. Fire-and-forget; errors are
	// logged but don't fail the create response.
	SendInitialPrompt(name, text string)
}

// CreateLookPath is the seam used for the "claude on PATH" check.
// exec.LookPath satisfies it in production.
type CreateLookPath interface {
	LookPath(file string) (string, error)
}

type createReqBody struct {
	Workdir       string `json:"workdir"`
	Name          string `json:"name,omitempty"`
	InitialPrompt string `json:"initial_prompt,omitempty"`
}

var createNameRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

// CreateSession returns POST /api/sessions.
func CreateSession(src InputSessionSource, sp CreateSpawner, lp CreateLookPath) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			writeInputErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "POST only")
			return
		}

		var body createReqBody
		r.Body = http.MaxBytesReader(w, r.Body, 4096)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeInputErr(w, http.StatusBadRequest, "invalid_body", err.Error())
			return
		}
		body.Workdir = strings.TrimSpace(body.Workdir)
		body.Name = strings.TrimSpace(body.Name)
		if body.Workdir == "" {
			writeInputErr(w, http.StatusBadRequest, "invalid_body", "workdir required")
			return
		}

		if !filepath.IsAbs(body.Workdir) {
			writeInputErr(w, http.StatusBadRequest, "workdir_not_absolute",
				"workdir must be an absolute path")
			return
		}
		info, err := os.Stat(body.Workdir)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				writeInputErr(w, http.StatusBadRequest, "bad_workdir",
					"workdir stat: "+err.Error())
				return
			}
			// Auto-create the workdir so users can spawn sessions for
			// directories that don't exist yet (fresh project scratchpad).
			if mkErr := os.MkdirAll(body.Workdir, 0o755); mkErr != nil {
				writeInputErr(w, http.StatusBadRequest, "bad_workdir",
					"workdir mkdir: "+mkErr.Error())
				return
			}
			info, err = os.Stat(body.Workdir)
			if err != nil {
				writeInputErr(w, http.StatusInternalServerError, "bad_workdir",
					"workdir stat after mkdir: "+err.Error())
				return
			}
		}
		if !info.IsDir() {
			writeInputErr(w, http.StatusBadRequest, "workdir_not_dir",
				"workdir is not a directory")
			return
		}
		if _, err := lp.LookPath("claude"); err != nil {
			writeInputErr(w, http.StatusServiceUnavailable, "no_claude",
				"claude CLI not found on PATH")
			return
		}

		name := body.Name
		if name == "" {
			name = filepath.Base(strings.TrimRight(body.Workdir, "/"))
		}
		if !createNameRe.MatchString(name) {
			writeInputErr(w, http.StatusBadRequest, "bad_name",
				"name must match ^[A-Za-z0-9._-]{1,64}$")
			return
		}

		if existing, ok := src.Get(name); ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":   "name_exists",
				"message": "a session named '" + name + "' already exists",
				"session": existing,
			})
			return
		}

		sess, err := sp.Spawn(name, body.Workdir)
		if err != nil {
			writeInputErr(w, http.StatusInternalServerError, "spawn_failed", err.Error())
			return
		}

		if prompt := strings.TrimRight(body.InitialPrompt, " \t\n\r"); prompt != "" {
			sp.SendInitialPrompt(sess.Name, prompt)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(sess)
	}
}
