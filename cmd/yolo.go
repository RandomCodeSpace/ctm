package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"
	"github.com/RandomCodeSpace/ctm/internal/claude"
	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/output"
	"github.com/RandomCodeSpace/ctm/internal/prompt"
	"github.com/RandomCodeSpace/ctm/internal/serve/proc"
	"github.com/RandomCodeSpace/ctm/internal/session"
	"github.com/RandomCodeSpace/ctm/internal/shell"
	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

func init() {
	rootCmd.AddCommand(yoloCmd)
	rootCmd.AddCommand(yoloBangCmd)
	rootCmd.AddCommand(safeCmd)
}

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
	upper := ""
	for _, r := range mode {
		if r >= 'a' && r <= 'z' {
			upper += string(r - 32)
		} else {
			upper += string(r)
		}
	}
	return fmt.Sprintf(">>> %s MODE", upper), false
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

var yoloCmd = &cobra.Command{
	Use:               "yolo [name] [path]",
	Short:             "Launch or relaunch a session in YOLO (unrestricted) mode",
	Args:              cobra.MaximumNArgs(2),
	ValidArgsFunction: shell.SessionNameCompletion(),
	RunE:              runYolo,
}

var yoloBangCmd = &cobra.Command{
	Use:               "yolo! [name]",
	Short:             "Force kill and relaunch a session in YOLO mode",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: shell.SessionNameCompletion(),
	RunE:              runYoloBang,
}

var safeCmd = &cobra.Command{
	Use:               "safe [name]",
	Short:             "Launch or relaunch a session in safe mode",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: shell.SessionNameCompletion(),
	RunE:              runSafe,
}

func runYolo(cmd *cobra.Command, args []string) error {
	proc.EnsureServeRunning(cmd.Context())
	out := output.Stdout()
	cfgPtr, err := ensureSetup()
	if err != nil {
		return err
	}
	cfg := *cfgPtr

	store := session.NewStore(config.SessionsPath())
	tc := tmux.NewClient(config.TmuxConfPath())

	var name, workdir string

	switch len(args) {
	case 0:
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting working directory: %w", err)
		}
		name = session.SanitizeName(filepath.Base(cwd))
		workdir = cwd
	case 1:
		name = args[0]
		// If session exists use its workdir, else prompt
		if sess, err := store.Get(name); err == nil {
			workdir = sess.Workdir
		} else {
			p, err := prompt.AskPath("Working directory: ")
			if err != nil {
				return fmt.Errorf("prompting for path: %w", err)
			}
			resolved, err := prompt.ResolvePath(p)
			if err != nil {
				return fmt.Errorf("resolving path: %w", err)
			}
			workdir = resolved
		}
	case 2:
		name = args[0]
		resolved, err := prompt.ResolvePath(args[1])
		if err != nil {
			return fmt.Errorf("resolving path: %w", err)
		}
		workdir = resolved
	}

	if err := session.ValidateName(name); err != nil {
		return err
	}

	if cfg.GitCheckpointBeforeYolo {
		out.Debug(Verbose, "git checkpoint for %s", workdir)
		gitCheckpoint(workdir, out)
	}

	printBanner(out, "yolo")
	fireLaunchEvents(store, name, workdir, "yolo")

	// If session exists and mode matches → preflight. preflight handles both
	// live tmux (plain reattach) and dead tmux (recreate with --resume UUID),
	// so the session's claude history survives `claude` exiting on its own.
	// Only kill/delete when the mode actually changes (safe → yolo) or when
	// the user forces fresh state via `ctm yolo!` / `ctm kill`.
	sess, getErr := store.Get(name)
	switch decideModeAction(sess, getErr, "yolo") {
	case decisionResume:
		out.Debug(Verbose, "existing yolo session %q — running pre-flight", name)
		return preflight(sess, cfg, store, tc, out)
	case decisionRecreate:
		// Mode change: drop tmux + store record so a fresh UUID is minted.
		tearDownForRecreate(name, store, tc, out, true)
	}

	out.Debug(Verbose, "creating yolo session: %s", name)
	return createAndAttach(name, workdir, "yolo", store, tc, out)
}

func runYoloBang(cmd *cobra.Command, args []string) error {
	proc.EnsureServeRunning(cmd.Context())
	out := output.Stdout()
	cfgPtr, err := ensureSetup()
	if err != nil {
		return err
	}
	cfg := *cfgPtr

	store := session.NewStore(config.SessionsPath())
	tc := tmux.NewClient(config.TmuxConfPath())

	name, workdir, err := resolveModeTarget(args, store, tc)
	if err != nil {
		return err
	}

	if cfg.GitCheckpointBeforeYolo {
		gitCheckpoint(workdir, out)
	}

	printBanner(out, "yolo")
	fireLaunchEvents(store, name, workdir, "yolo")

	// `yolo!` forces fresh state unconditionally — store.Delete errors are
	// swallowed (loud=false) because the historic behavior treated this as
	// a best-effort reset.
	tearDownForRecreate(name, store, tc, out, false)

	return createAndAttach(name, workdir, "yolo", store, tc, out)
}

func runSafe(cmd *cobra.Command, args []string) error {
	proc.EnsureServeRunning(cmd.Context())
	out := output.Stdout()
	cfgPtr, err := ensureSetup()
	if err != nil {
		return err
	}
	cfg := *cfgPtr

	store := session.NewStore(config.SessionsPath())
	tc := tmux.NewClient(config.TmuxConfPath())

	name, workdir, err := resolveModeTarget(args, store, tc)
	if err != nil {
		return err
	}

	printBanner(out, "safe")
	fireLaunchEvents(store, name, workdir, "safe")

	// If session exists and mode matches → preflight. preflight handles both
	// live tmux (plain reattach) and dead tmux (recreate with --resume UUID),
	// so the session's claude history survives `claude` exiting on its own.
	// Force-fresh escape hatches: `ctm kill <name>` / `ctm forget <name>`.
	sess, getErr := store.Get(name)
	switch decideModeAction(sess, getErr, "safe") {
	case decisionResume:
		out.Debug(Verbose, "existing safe session %q — running pre-flight", name)
		return preflight(sess, cfg, store, tc, out)
	case decisionRecreate:
		// safe matches yolo's silent-on-delete-failure historical behavior.
		tearDownForRecreate(name, store, tc, out, false)
	}

	return createAndAttach(name, workdir, "safe", store, tc, out)
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

// gitCheckpoint creates a git checkpoint commit in workdir before yolo mode.
func gitCheckpoint(workdir string, out *output.Printer) {
	check := exec.Command("git", "-C", workdir, "rev-parse", "--is-inside-work-tree")
	if err := check.Run(); err != nil {
		out.Dim("(not a git repo — skipping checkpoint)")
		return
	}

	exec.Command("git", "-C", workdir, "add", "-A").Run() //nolint:errcheck

	ts := time.Now().Format("2006-01-02T15:04:05")
	msg := fmt.Sprintf("checkpoint: pre-yolo %s", ts)
	exec.Command("git", "-C", workdir, "commit", "-m", msg, "--allow-empty", "-q").Run() //nolint:errcheck

	out.Dim("git checkpoint created — to rollback: git -C %s reset --hard HEAD~1", workdir)
}

// Ensure shell import is used (completion helper comes from shell package).
var _ = claude.BuildCommand
