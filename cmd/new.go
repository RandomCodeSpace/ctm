package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/output"
	"github.com/RandomCodeSpace/ctm/internal/prompt"
	"github.com/RandomCodeSpace/ctm/internal/session"
	"github.com/RandomCodeSpace/ctm/internal/shell"
	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

func init() {
	rootCmd.AddCommand(newCmd)
}

var newCmd = &cobra.Command{
	Use:               "new [name] [path]",
	Short:             "Create a new session (fresh conversation)",
	Args:              cobra.MaximumNArgs(2),
	ValidArgsFunction: shell.SessionNameCompletion(),
	RunE:              runNew,
}

func runNew(cmd *cobra.Command, args []string) error {
	out := output.Stderr()
	cfg, err := config.Load(config.ConfigPath())
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	store := session.NewStore(config.SessionsPath())
	tc := tmux.NewClient(config.TmuxConfPath())

	if err := tmux.GenerateConfig(config.TmuxConfPath(), cfg.ScrollbackLines); err != nil {
		return fmt.Errorf("generating tmux config: %w", err)
	}

	var name, workdir string

	switch len(args) {
	case 0:
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("getting current directory: %w", err)
		}
		workdir = cwd
		name = session.SanitizeName(filepath.Base(cwd))
	case 1:
		name = args[0]
		raw, err := prompt.AskPath("workdir: ")
		if err != nil {
			return fmt.Errorf("reading path: %w", err)
		}
		resolved, err := prompt.ResolvePath(raw)
		if err != nil {
			return fmt.Errorf("resolving path: %w", err)
		}
		workdir = resolved
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

	// Kill existing session with the same name if present
	if _, err := store.Get(name); err == nil {
		out.Warn("session %q already exists — replacing", name)
		_ = tc.KillSession(name)
		if err := store.Delete(name); err != nil {
			out.Warn("could not delete old session record: %v", err)
		}
	}

	return createAndAttach(name, workdir, cfg.DefaultMode, store, tc, out)
}
