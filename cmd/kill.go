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
	rootCmd.AddCommand(killCmd)
	rootCmd.AddCommand(killAllCmd)
}

var killCmd = &cobra.Command{
	Use:               "kill <name>",
	Short:             "Kill a session and remove it from the store",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: shell.SessionNameCompletion(),
	RunE:              runKill,
}

var killAllCmd = &cobra.Command{
	Use:   "killall",
	Short: "Kill the tmux server and remove all sessions from the store",
	Args:  cobra.NoArgs,
	RunE:  runKillAll,
}

func runKill(cmd *cobra.Command, args []string) error {
	name := args[0]
	out := output.Stderr()
	store := session.NewStore(config.SessionsPath())
	tc := tmux.NewClient(config.TmuxConfPath())

	if _, err := store.Get(name); err != nil {
		return fmt.Errorf("session %q not found", name)
	}

	if tc.HasSession(name) {
		if err := tc.KillSession(name); err != nil {
			out.Warn("could not kill tmux session %q: %v", name, err)
		}
	}

	if err := store.Delete(name); err != nil {
		return fmt.Errorf("removing session from store: %w", err)
	}

	out.Success("killed session %q", name)
	return nil
}

func runKillAll(cmd *cobra.Command, args []string) error {
	out := output.Stderr()
	store := session.NewStore(config.SessionsPath())
	tc := tmux.NewClient(config.TmuxConfPath())

	sessions, err := store.List()
	if err != nil {
		return fmt.Errorf("listing sessions: %w", err)
	}

	if len(sessions) == 0 {
		out.Dim("No sessions to kill")
		return nil
	}

	if err := tc.KillServer(); err != nil {
		out.Warn("could not kill tmux server: %v", err)
	}

	if err := store.DeleteAll(); err != nil {
		return fmt.Errorf("clearing session store: %w", err)
	}

	out.Success("killed all sessions (%d)", len(sessions))
	return nil
}
