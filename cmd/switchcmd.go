package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/output"
	"github.com/RandomCodeSpace/ctm/internal/session"
	"github.com/RandomCodeSpace/ctm/internal/shell"
	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

func init() {
	rootCmd.AddCommand(switchCmd)
}

var switchCmd = &cobra.Command{
	Use:               "switch [name]",
	Aliases:           []string{"sw"},
	Short:             "Switch to another session",
	Args:              cobra.MaximumNArgs(1),
	ValidArgsFunction: shell.SessionNameCompletion(),
	RunE:              runSwitch,
}

func runSwitch(cmd *cobra.Command, args []string) error {
	tc := tmux.NewClient(config.TmuxConfPath())

	// No args
	if len(args) == 0 {
		if tmux.IsInsideTmux() {
			return tc.ChooseSession()
		}
		return runList(cmd, args)
	}

	name := args[0]
	if err := session.ValidateName(name); err != nil {
		return err
	}

	out := output.Stderr()
	cfg, err := config.Load(config.ConfigPath())
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	store := session.NewStore(config.SessionsPath())

	sess, err := store.Get(name)
	if err != nil {
		// Not in store — check tmux directly
		if !tc.HasSession(name) {
			return fmt.Errorf("session %q not found", name)
		}
		return tc.Go(name)
	}

	_ = out
	return preflight(sess, cfg, store, tc, out)
}
