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
	"github.com/RandomCodeSpace/ctm/internal/session"
	"github.com/RandomCodeSpace/ctm/internal/shell"
	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

func init() {
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
	Short:             "Force kill and relaunch a session in safe mode",
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

	if cfg.GitCheckpointBeforeYolo {
		out.Debug(Verbose, "git checkpoint for %s", workdir)
		gitCheckpoint(workdir, out)
	}

	out.Magenta(">>> YOLO MODE")

	// If session exists + running + already yolo → run pre-flight then attach
	if sess, err := store.Get(name); err == nil {
		if sess.Mode == "yolo" && tc.HasSession(name) {
			out.Debug(Verbose, "session already yolo, running pre-flight")
			return preflight(sess, cfg, store, tc, out)
		}
		// Kill existing
		if tc.HasSession(name) {
			if err := tc.KillSession(name); err != nil {
				out.Warn("could not kill existing session: %v", err)
			}
		}
		if err := store.Delete(name); err != nil {
			out.Warn("could not remove session from store: %v", err)
		}
	}

	out.Debug(Verbose, "creating yolo session: %s", name)
	return createAndAttach(name, workdir, "yolo", store, tc, out)
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

	name := "claude"
	if len(args) > 0 {
		name = args[0]
	}
	if err := session.ValidateName(name); err != nil {
		return err
	}

	// Get workdir from existing session or pane path
	workdir, err := resolveWorkdir(name, store, tc)
	if err != nil {
		return err
	}

	if cfg.GitCheckpointBeforeYolo {
		gitCheckpoint(workdir, out)
	}

	out.Magenta(">>> YOLO MODE")

	if tc.HasSession(name) {
		if err := tc.KillSession(name); err != nil {
			out.Warn("could not kill existing session: %v", err)
		}
	}
	if err := store.Delete(name); err != nil {
		// ignore "not found" errors
		_ = err
	}

	return createAndAttach(name, workdir, "yolo", store, tc, out)
}

func runSafe(cmd *cobra.Command, args []string) error {
	out := output.Stdout()
	if _, err := ensureSetup(); err != nil {
		return err
	}

	store := session.NewStore(config.SessionsPath())
	tc := tmux.NewClient(config.TmuxConfPath())

	name := "claude"
	if len(args) > 0 {
		name = args[0]
	}
	if err := session.ValidateName(name); err != nil {
		return err
	}

	// Get workdir from existing session or pane path
	workdir, err := resolveWorkdir(name, store, tc)
	if err != nil {
		return err
	}

	out.Success(">>> SAFE MODE")

	if tc.HasSession(name) {
		if err := tc.KillSession(name); err != nil {
			out.Warn("could not kill existing session: %v", err)
		}
	}
	if err := store.Delete(name); err != nil {
		// ignore "not found" errors
		_ = err
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
