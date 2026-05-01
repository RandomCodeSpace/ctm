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

// modeContext bundles everything runYoloBang + runSafe need before they
// branch into mode-specific work: serve started, config loaded, store +
// tmux client constructed, name resolved (default "claude"), workdir
// resolved via resolveWorkdir. runYolo doesn't fit this shape — it
// resolves workdir from up-to-two positional args.
type modeContext struct {
	out     *output.Printer
	cfg     config.Config
	store   *session.Store
	tc      *tmux.Client
	name    string
	workdir string
}

func setupModeContext(cmd *cobra.Command, args []string) (*modeContext, error) {
	proc.EnsureServeRunning(cmd.Context())
	out := output.Stdout()
	cfgPtr, err := ensureSetup()
	if err != nil {
		return nil, err
	}

	store := session.NewStore(config.SessionsPath())
	tc := tmux.NewClient(config.TmuxConfPath())

	name := "claude"
	if len(args) > 0 {
		name = args[0]
	}
	if err := session.ValidateName(name); err != nil {
		return nil, err
	}

	workdir, err := resolveWorkdir(name, store, tc)
	if err != nil {
		return nil, err
	}

	return &modeContext{out: out, cfg: *cfgPtr, store: store, tc: tc, name: name, workdir: workdir}, nil
}

// modeAttachOptions encodes the per-mode differences in the resume-or-
// recreate dance shared by runYolo and runSafe: banner color, lifecycle
// event names, and whether a failed store.Delete is loud.
type modeAttachOptions struct {
	mode       string // "yolo" or "safe"
	bannerYolo bool   // true → out.Magenta; false → out.Success
	bannerText string // ">>> YOLO MODE" / ">>> SAFE MODE"
	hookEvent  string // "on_yolo" / "on_safe"
	serveEvent string // "on_yolo" / "session_attached"
	// loudOnDeleteErr: yolo warns when store.Delete fails because losing
	// the prior record matters for safety auditing; safe-mode demotion
	// silently swallows it (the user is already moving away from a
	// degraded state).
	loudOnDeleteErr bool
}

func runResumeOrRecreate(opts modeAttachOptions, name, workdir string, cfg config.Config, store *session.Store, tc *tmux.Client, out *output.Printer) error {
	if opts.bannerYolo {
		out.Magenta("%s", opts.bannerText)
	} else {
		out.Success("%s", opts.bannerText)
	}
	intent := yoloIntent(store, name, workdir, opts.mode)
	fireHook(opts.hookEvent, intent)
	fireServeEvent(opts.serveEvent, intent)

	// If session exists and mode matches → preflight. preflight handles both
	// live tmux (plain reattach) and dead tmux (recreate with --resume UUID),
	// so the session's claude history survives `claude` exiting on its own.
	// Mode change (safe ↔ yolo) drops tmux + store record so a fresh UUID
	// is minted; force-fresh escape hatches: `ctm yolo!` / `ctm kill <name>`
	// / `ctm forget <name>`.
	if sess, err := store.Get(name); err == nil {
		if shouldResumeExisting(sess, opts.mode) {
			out.Debug(Verbose, "existing %s session %q — running pre-flight", opts.mode, name)
			return preflight(sess, cfg, store, tc, out)
		}
		if tc.HasSession(name) {
			if err := tc.KillSession(name); err != nil {
				out.Warn("could not kill existing session: %v", err)
			}
		}
		if err := store.Delete(name); err != nil {
			if opts.loudOnDeleteErr {
				out.Warn("could not remove session from store: %v", err)
			}
		}
	}

	out.Debug(Verbose, "creating %s session: %s", opts.mode, name)
	return createAndAttach(name, workdir, opts.mode, store, tc, out)
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

	return runResumeOrRecreate(modeAttachOptions{
		mode:            "yolo",
		bannerYolo:      true,
		bannerText:      ">>> YOLO MODE",
		hookEvent:       "on_yolo",
		serveEvent:      "on_yolo",
		loudOnDeleteErr: true,
	}, name, workdir, cfg, store, tc, out)
}

func runYoloBang(cmd *cobra.Command, args []string) error {
	mc, err := setupModeContext(cmd, args)
	if err != nil {
		return err
	}

	if mc.cfg.GitCheckpointBeforeYolo {
		gitCheckpoint(mc.workdir, mc.out)
	}

	mc.out.Magenta(">>> YOLO MODE")
	intent := yoloIntent(mc.store, mc.name, mc.workdir, "yolo")
	fireHook("on_yolo", intent)
	fireServeEvent("on_yolo", intent)

	if mc.tc.HasSession(mc.name) {
		if err := mc.tc.KillSession(mc.name); err != nil {
			mc.out.Warn("could not kill existing session: %v", err)
		}
	}
	if err := mc.store.Delete(mc.name); err != nil {
		// ignore "not found" errors — yolo! is force-recreate by design
		_ = err
	}

	return createAndAttach(mc.name, mc.workdir, "yolo", mc.store, mc.tc, mc.out)
}

func runSafe(cmd *cobra.Command, args []string) error {
	mc, err := setupModeContext(cmd, args)
	if err != nil {
		return err
	}

	return runResumeOrRecreate(modeAttachOptions{
		mode:            "safe",
		bannerYolo:      false,
		bannerText:      ">>> SAFE MODE",
		hookEvent:       "on_safe",
		// "on_safe" maps to a serve session_attached — the hub doesn't
		// model safe-mode separately, only the lifecycle transition.
		serveEvent:      "session_attached",
		loudOnDeleteErr: false,
	}, mc.name, mc.workdir, mc.cfg, mc.store, mc.tc, mc.out)
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
