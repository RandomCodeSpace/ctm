// Package hooks dispatches user-configured shell commands on ctm
// lifecycle events (attach, new, yolo, safe, kill).
//
// The contract is deliberately narrow: hooks are shell strings, one
// per named event, declared in config.json under "hooks". Each hook
// runs synchronously with a configurable timeout (default 5 s) and
// receives the session context via CTM_* environment variables. A
// hook that errors or times out logs a WARN-level diagnostic but
// never blocks or rolls back the action that fired it — hooks are
// side-channel notifications, not gates.
//
// To run something in the background, the user writes a trailing `&`
// or spawns a backgrounded process from the hook command.
package hooks

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Supported event names. Unknown events in config log a warning at
// startup but are never invoked — see IsKnownEvent.
const (
	EventOnAttach = "on_attach"
	EventOnNew    = "on_new"
	EventOnYolo   = "on_yolo"
	EventOnSafe   = "on_safe"
	EventOnKill   = "on_kill"
)

// DefaultTimeout is the per-hook wall-clock ceiling. 5 s is short
// enough to stay out of the interactive path's latency budget but
// long enough for a curl or a git commit.
const DefaultTimeout = 5 * time.Second

var knownEvents = map[string]struct{}{
	EventOnAttach: {},
	EventOnNew:    {},
	EventOnYolo:   {},
	EventOnSafe:   {},
	EventOnKill:   {},
}

// IsKnownEvent reports whether name is in the documented event set.
func IsKnownEvent(name string) bool {
	_, ok := knownEvents[name]
	return ok
}

// Context is the per-invocation data passed into hooks as environment
// variables. Any zero-valued field is translated to an empty env var
// — hooks must tolerate missing data rather than crash on it.
type Context struct {
	SessionName    string
	SessionUUID    string
	SessionMode    string
	SessionWorkdir string
}

// Run executes the hook command bound to event (via hooks map from
// config). If event has no entry, Run is a silent no-op. The hook is
// invoked through "sh -c" so users can use pipes, redirects, and env
// expansion. Timeout defaults to DefaultTimeout; pass <=0 to use it.
//
// Returns nil on success and a non-nil error on any failure (timeout,
// non-zero exit, command not found). Callers in ctm's command path
// should swallow — a failing hook must never block the action that
// triggered it. Internal WARN-level slog lines make the failure
// observable via --log-level=info or --verbose.
func Run(event string, hooks map[string]string, cctx Context, timeout time.Duration) error {
	cmdStr, ok := hooks[event]
	if !ok || strings.TrimSpace(cmdStr) == "" {
		return nil
	}
	if !IsKnownEvent(event) {
		slog.Warn("hooks: ignoring unknown event name", "event", event)
		return nil
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Env = append(os.Environ(),
		"CTM_EVENT="+event,
		"CTM_SESSION_NAME="+cctx.SessionName,
		"CTM_SESSION_UUID="+cctx.SessionUUID,
		"CTM_SESSION_MODE="+cctx.SessionMode,
		"CTM_SESSION_WORKDIR="+cctx.SessionWorkdir,
	)

	started := time.Now()
	err := cmd.Run()
	elapsed := time.Since(started)

	if err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			slog.Warn("hook timed out",
				"event", event, "timeout", timeout, "elapsed", elapsed)
		} else {
			slog.Warn("hook failed",
				"event", event, "err", err, "elapsed", elapsed)
		}
		return err
	}
	slog.Debug("hook ran",
		"event", event, "elapsed", elapsed)
	return nil
}
