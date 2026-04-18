// Package logging configures ctm's structured diagnostic logger.
//
// ctm has two distinct output channels. User-facing status messages
// (success, error, dim hints) go through internal/output.Printer on
// stdout and have always done so. Diagnostic lines — warnings about
// recoverable state issues, stripped unknown config keys, debug traces
// — go through log/slog on stderr, controlled by --log-level.
//
// The two channels are deliberate. Anyone scripting against ctm pipes
// its stdout; filling stdout with diagnostic noise would break scripts.
// stderr is where an operator looks when something misbehaves.
//
// At the default INFO level, stderr stays effectively silent for
// successful runs — only WARN/ERROR fire from normal paths.
package logging

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
)

// Env vars honoured by Setup.
const (
	// EnvFormat selects the slog handler. "text" (default) or "json".
	EnvFormat = "CTM_LOG_FORMAT"
)

var setupOnce sync.Once

// Setup configures the default slog.Logger to write structured
// diagnostics to stderr at the given level. Parseable levels (case-
// insensitive): "debug", "info", "warn", "error". An empty string is
// treated as "info".
//
// It is safe to call multiple times; only the first call has effect so
// that subcommands calling Setup in their own PreRun don't clobber a
// root-level configuration. Tests can bypass the once guard with
// ResetForTest.
//
// Format defaults to text (human-readable). Set CTM_LOG_FORMAT=json to
// emit newline-delimited JSON suitable for log aggregators.
func Setup(level string) error {
	lvl, err := ParseLevel(level)
	if err != nil {
		return err
	}

	var handlerErr error
	setupOnce.Do(func() {
		handlerErr = applyHandler(lvl)
	})
	return handlerErr
}

// ResetForTest re-arms Setup so the next call takes effect. Tests only.
func ResetForTest() {
	setupOnce = sync.Once{}
}

// ParseLevel turns a user-supplied level string into a slog.Level.
// Accepts the stdlib names case-insensitively; empty string → INFO.
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error", "err":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("unknown log level %q (want debug|info|warn|error)", s)
}

func applyHandler(lvl slog.Level) error {
	opts := &slog.HandlerOptions{Level: lvl}

	var h slog.Handler
	switch strings.ToLower(os.Getenv(EnvFormat)) {
	case "json":
		h = slog.NewJSONHandler(os.Stderr, opts)
	default:
		h = slog.NewTextHandler(os.Stderr, opts)
	}
	slog.SetDefault(slog.New(h))
	return nil
}
