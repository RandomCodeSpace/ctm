package session

// V26 — reusable yolo-spawn. Lifted from cmd/yolo.go's createAndAttach
// so the HTTP /api/sessions create endpoint shares a code path with
// the CLI. Attach is intentionally NOT part of this function — the
// daemon never attaches; the CLI wraps this + Attach.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/RandomCodeSpace/ctm/internal/agent/claude"
)

// TmuxSpawner is the narrow slice of *tmux.Client Yolo needs.
type TmuxSpawner interface {
	NewSession(name, workdir, shellCmd string) error
	SendKeys(target, keys string) error
	KillSession(name string) error
}

// Saver is the narrow slice of *Store Yolo needs.
type Saver interface {
	Save(sess *Session) error
}

// SpawnOpts bundles the tmux client and store so Yolo can be driven
// from either CLI or daemon without depending on config globals.
//
// EnvExports is a pre-built shell-export prelude (e.g.
// "export CLAUDE_CODE_NO_FLICKER='1' CTM_STATUSLINE_DUMP='/tmp/...'")
// produced by config.ClaudeEnvExports(). Empty when claude-env.json
// is absent or has no entries.
type SpawnOpts struct {
	Name        string
	Workdir     string
	Tmux        TmuxSpawner
	Store       Saver
	OverlayPath string
	EnvExports  string
}

// Yolo creates a detached tmux session, launches claude in yolo mode,
// and persists the session state. Returns the populated Session.
//
// Preconditions:
//   - Workdir must be absolute
//   - Workdir must exist and be a directory
//
// Postconditions on success: tmux session exists detached, claude is
// launched inside it, Store.Save has been called with mode="yolo".
// On error before Save: NO session state is persisted.
func Yolo(opts SpawnOpts) (Session, error) {
	if !filepath.IsAbs(opts.Workdir) {
		return Session{}, fmt.Errorf("workdir must be absolute: %q", opts.Workdir)
	}
	info, err := os.Stat(opts.Workdir)
	if err != nil {
		return Session{}, fmt.Errorf("workdir stat: %w", err)
	}
	if !info.IsDir() {
		return Session{}, fmt.Errorf("workdir is not a directory: %q", opts.Workdir)
	}

	uid := newUUIDv4()
	shellCmd := claude.BuildCommand(uid, "yolo", false, opts.OverlayPath, opts.EnvExports)

	if err := opts.Tmux.NewSession(opts.Name, opts.Workdir, shellCmd); err != nil {
		return Session{}, fmt.Errorf("tmux new-session: %w", err)
	}

	sess := Session{
		Name:    opts.Name,
		UUID:    uid,
		Mode:    "yolo",
		Workdir: opts.Workdir,
	}
	if err := opts.Store.Save(&sess); err != nil {
		// Best-effort cleanup of the orphan tmux session we just created.
		// Ignore the kill error — Save's err is the meaningful one.
		_ = opts.Tmux.KillSession(opts.Name)
		return Session{}, fmt.Errorf("session save: %w", err)
	}
	return sess, nil
}
