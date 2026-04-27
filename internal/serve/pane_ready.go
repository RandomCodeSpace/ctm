package serve

import (
	"context"
	"strings"
	"time"
)

// paneCapturer is the minimal surface waitForPaneReady needs — a
// single CapturePane call that returns the current visible pane
// contents (ANSI-preserving, same shape as *tmux.Client.CapturePane).
type paneCapturer interface {
	CapturePane(target string) (string, error)
}

// waitOpts carries the tunables + test seams for waitForPaneReady.
// Zero values pick sensible production defaults.
type waitOpts struct {
	interval time.Duration
	timeout  time.Duration
	now      func() time.Time
	sleep    func(time.Duration)
}

// defaults for waitOpts — chosen so SendInitialPrompt has the same
// worst-case ceiling as a corporate-slow cold start, while a fast
// machine returns in hundreds of ms instead of the old fixed 8s.
const (
	defaultPaneReadyInterval = 200 * time.Millisecond
	defaultPaneReadyTimeout  = 15 * time.Second
)

// waitForPaneReady polls the named tmux target until two consecutive
// captures are byte-identical AND non-empty (trimmed), signalling the
// TUI has rendered and stopped churning. Returns nil on readiness,
// context.DeadlineExceeded on timeout. Capture errors do not abort —
// they reset the "previous" snapshot so we keep polling until the
// budget runs out. The helper is deliberately dependency-light so
// callers can inject fakes in tests.
func waitForPaneReady(ctx context.Context, tmux paneCapturer, target string, opts waitOpts) error {
	if opts.interval <= 0 {
		opts.interval = defaultPaneReadyInterval
	}
	if opts.timeout <= 0 {
		opts.timeout = defaultPaneReadyTimeout
	}
	if opts.now == nil {
		opts.now = time.Now
	}
	if opts.sleep == nil {
		opts.sleep = time.Sleep
	}

	deadline := opts.now().Add(opts.timeout)
	var prev string
	havePrev := false

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		cur, err := tmux.CapturePane(target)
		if err != nil {
			// Transient capture failure (pane not yet attached, tmux
			// racing). Drop any prior snapshot so we don't falsely
			// match across an error, then keep polling.
			havePrev = false
			prev = ""
		} else if havePrev && cur == prev && len(strings.TrimSpace(cur)) > 0 {
			return nil
		} else {
			prev = cur
			havePrev = true
		}

		if !opts.now().Before(deadline) {
			return context.DeadlineExceeded
		}
		opts.sleep(opts.interval)
		if !opts.now().Before(deadline) {
			return context.DeadlineExceeded
		}
	}
}
