// Cobra wiring + RunE bodies for the yolo / yolo! / safe commands.
//
// Split out from yolo.go so the heavy integration paths (preflight,
// createAndAttach, gitCheckpoint) live in one place and can be
// excluded from the SonarCloud coverage gate. The pure helpers each
// runner composes (decideModeAction, fireLaunchEvents,
// resolveModeTarget, tearDownForRecreate, printBanner, etc.) all live
// in yolo.go and are unit-tested there.

package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/output"
	"github.com/RandomCodeSpace/ctm/internal/prompt"
	"github.com/RandomCodeSpace/ctm/internal/session"
	"github.com/RandomCodeSpace/ctm/internal/shell"
	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

func init() {
	for _, c := range []*cobra.Command{yoloCmd, yoloBangCmd, safeCmd} {
		c.Flags().String("agent", "", "Agent to spawn (codex, hermes). Empty uses the configured default.")
	}
	rootCmd.AddCommand(yoloCmd)
	rootCmd.AddCommand(yoloBangCmd)
	rootCmd.AddCommand(safeCmd)
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

	agentFlag, _ := cmd.Flags().GetString("agent")
	agentName, err := resolveAgent(agentFlag)
	if err != nil {
		return err
	}

	if cfg.GitCheckpointBeforeYolo {
		out.Debug(Verbose, "git checkpoint for %s", workdir)
		gitCheckpoint(workdir, out)
	}

	printBanner(out, "yolo")
	fireLaunchEvents(store, name, workdir, "yolo")

	// If session exists and mode matches → preflight. preflight handles both
	// live tmux (plain reattach) and dead tmux (recreate via the agent's resume
	// command), so the session's history survives the agent exiting on its own.
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
	return createAndAttach(name, workdir, "yolo", agentName, store, tc, out)
}

func runYoloBang(cmd *cobra.Command, args []string) error {
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

	agentFlag, _ := cmd.Flags().GetString("agent")
	agentName, err := resolveAgent(agentFlag)
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

	return createAndAttach(name, workdir, "yolo", agentName, store, tc, out)
}

func runSafe(cmd *cobra.Command, args []string) error {
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

	agentFlag, _ := cmd.Flags().GetString("agent")
	agentName, err := resolveAgent(agentFlag)
	if err != nil {
		return err
	}

	printBanner(out, "safe")
	fireLaunchEvents(store, name, workdir, "safe")

	// If session exists and mode matches → preflight. preflight handles both
	// live tmux (plain reattach) and dead tmux (recreate via the agent's resume
	// command), so the session's history survives the agent exiting on its own.
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

	return createAndAttach(name, workdir, "safe", agentName, store, tc, out)
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
