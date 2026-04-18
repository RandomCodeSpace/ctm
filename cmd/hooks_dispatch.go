package cmd

import (
	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/hooks"
	"github.com/RandomCodeSpace/ctm/internal/session"
)

// yoloIntent builds a Session value suitable for passing into fireHook
// when we're about to launch yolo/safe on a session that may or may
// not exist yet in the store. If one exists with a matching name we
// return its stored state (preserving UUID etc.); otherwise we return
// a minimal Session describing the intended launch. Either way the
// hook receives a meaningful name/workdir/mode.
func yoloIntent(store *session.Store, name, workdir, mode string) *session.Session {
	if sess, err := store.Get(name); err == nil {
		return sess
	}
	return &session.Session{Name: name, Workdir: workdir, Mode: mode}
}

// fireHook dispatches a lifecycle event to any user-configured shell
// command bound under cfg.Hooks[event]. Errors are swallowed — a
// failing hook must never block the action that triggered it. The
// hook package already emits WARN-level slog diagnostics on failure,
// so the failure remains observable via --log-level.
//
// A nil session is tolerated — the relevant CTM_SESSION_* env vars
// are passed as empty strings so the hook script can branch on them.
//
// The config is reloaded here rather than threaded from each caller
// so the helper is a single-line wire-up at every site. The extra
// file read is trivial compared to the shell-exec in the hook path
// itself.
func fireHook(event string, sess *session.Session) {
	cfg, err := config.Load(config.ConfigPath())
	if err != nil || len(cfg.Hooks) == 0 {
		return
	}
	var hctx hooks.Context
	if sess != nil {
		hctx = hooks.Context{
			SessionName:    sess.Name,
			SessionUUID:    sess.UUID,
			SessionMode:    sess.Mode,
			SessionWorkdir: sess.Workdir,
		}
	}
	_ = hooks.Run(event, cfg.Hooks, hctx, cfg.HookTimeout())
}
