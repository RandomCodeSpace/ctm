// Pure helpers shared by the yolo / yolo! / safe runners. The cobra
// wiring + RunE bodies live in yolo_runners.go so the tmux- and
// integration-bound code can be excluded from the SonarCloud coverage
// gate (it's not unit-testable without a live tmux server).
//
// Everything in this file is deliberately side-effect-free or
// surgically scoped (one store call, one tmux client call) so it can
// be exercised by yolo_helpers_test.go.

package cmd

import (
	"fmt"
	"os"

	"github.com/RandomCodeSpace/ctm/internal/output"
	"github.com/RandomCodeSpace/ctm/internal/session"
	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

// shouldResumeExisting reports whether a stored session should be resumed via
// preflight rather than torn down and recreated. A session is resumable iff
// its recorded mode matches the requested mode — tmux liveness is irrelevant
// because preflight handles a dead tmux pane by recreating it with
// `claude --resume UUID`, preserving the session's conversation history.
//
// Regression guard: the previous implementation also required the tmux session
// to be live, which caused `ctm yolo <name>` after claude exited to delete the
// stored UUID and spawn a fresh session, losing all chat history.
func shouldResumeExisting(sess *session.Session, requestedMode string) bool {
	return sess != nil && sess.Mode == requestedMode
}

// modeDecision is the action a yolo/safe launch must take given the
// state of the store at launch time.
type modeDecision int

const (
	// decisionFresh: no stored session — create from scratch.
	decisionFresh modeDecision = iota
	// decisionResume: stored session matches requested mode — preflight + reattach.
	decisionResume
	// decisionRecreate: stored session is in a different mode — kill+delete then create.
	decisionRecreate
)

// decideModeAction maps the (store-lookup result, requested mode) pair to
// one of three actions. Pure function — easy to unit-test.
func decideModeAction(sess *session.Session, getErr error, requestedMode string) modeDecision {
	if getErr != nil {
		return decisionFresh
	}
	if shouldResumeExisting(sess, requestedMode) {
		return decisionResume
	}
	return decisionRecreate
}

// bannerFor returns the banner text and styling flag for a given launch mode.
// magenta=true → out.Magenta; false → out.Success (green). Unknown modes fall
// back to safe-style green so the screen never goes silent.
func bannerFor(mode string) (text string, magenta bool) {
	if mode == "yolo" {
		return ">>> YOLO MODE", true
	}
	upper := make([]byte, 0, len(mode))
	for i := 0; i < len(mode); i++ {
		c := mode[i]
		if c >= 'a' && c <= 'z' {
			upper = append(upper, c-32)
		} else {
			upper = append(upper, c)
		}
	}
	return fmt.Sprintf(">>> %s MODE", string(upper)), false
}

// eventsFor returns the (user-hook event, serve-hub event) pair for a mode.
// Yolo fires "on_yolo" to both. Safe fires "on_safe" to user hooks but maps
// to "session_attached" on the serve hub — the hub does not model a separate
// safe-mode lifecycle, only the attach transition.
func eventsFor(mode string) (hookEvent, serveEvent string) {
	if mode == "yolo" {
		return "on_yolo", "on_yolo"
	}
	return "on_" + mode, "session_attached"
}

// fireLaunchEvents fires both the user-defined shell hook and the serve-hub
// event for a launch in the given mode. Failures inside fireHook /
// fireServeEvent are already swallowed; this wrapper just composes them.
func fireLaunchEvents(store *session.Store, name, workdir, mode string) {
	hookEvent, serveEvent := eventsFor(mode)
	intent := yoloIntent(store, name, workdir, mode)
	fireHook(hookEvent, intent)
	fireServeEvent(serveEvent, intent)
}

// resolveSimpleName returns args[0] when present, else "claude". This is the
// name-resolution rule shared by `ctm yolo!` and `ctm safe`. (`ctm yolo` has a
// richer rule that also handles 2-arg form and prompts for a path, so it
// stays inline.)
func resolveSimpleName(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return "claude"
}

// resolveModeTarget produces the (name, workdir) pair used by `ctm yolo!` and
// `ctm safe`. Validates the name and resolves the workdir from the store, the
// running tmux pane, or the current working directory in that order.
func resolveModeTarget(args []string, store *session.Store, tc *tmux.Client) (string, string, error) {
	name := resolveSimpleName(args)
	if err := session.ValidateName(name); err != nil {
		return "", "", err
	}
	workdir, err := resolveWorkdir(name, store, tc)
	if err != nil {
		return "", "", err
	}
	return name, workdir, nil
}

// tearDownForRecreate drops the tmux session and store record so that a fresh
// UUID can be minted. Used when the requested mode differs from the stored
// mode, or when `ctm yolo!` forces fresh state.
//
// loudOnDeleteErr controls the original yolo/safe asymmetry: `ctm yolo`
// warns on a store.Delete failure; `ctm yolo!` swallows the error (it's a
// force-reset path). Preserved verbatim so this is a pure refactor.
func tearDownForRecreate(name string, store *session.Store, tc *tmux.Client, out *output.Printer, loudOnDeleteErr bool) {
	if tc.HasSession(name) {
		if err := tc.KillSession(name); err != nil {
			out.Warn("could not kill existing session: %v", err)
		}
	}
	if err := store.Delete(name); err != nil {
		if loudOnDeleteErr {
			out.Warn("could not remove session from store: %v", err)
		}
		// Silent branch: `ctm yolo!` ignores not-found and IO errors here.
		_ = err
	}
}

// printBanner prints the launch banner using the appropriate color for mode.
// We pass the text via `%s` so the banner string is never treated as a format
// string — defensive against future refactors where the banner becomes
// data-driven (silences `go vet` non-constant format string warnings).
func printBanner(out *output.Printer, mode string) {
	text, magenta := bannerFor(mode)
	if magenta {
		out.Magenta("%s", text)
	} else {
		out.Success("%s", text)
	}
}

// resolveWorkdir returns the workdir for name: from store if present, else from
// tmux pane path, else current working directory.
func resolveWorkdir(name string, store *session.Store, tc *tmux.Client) (string, error) {
	if sess, err := store.Get(name); err == nil {
		return sess.Workdir, nil
	}
	if tc.HasSession(name) {
		if p, err := tc.PaneCurrentPath(name); err == nil && p != "" {
			return p, nil
		}
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting working directory: %w", err)
	}
	return cwd, nil
}
