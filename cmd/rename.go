package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/output"
	"github.com/RandomCodeSpace/ctm/internal/session"
	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

func init() {
	rootCmd.AddCommand(renameCmd)
}

var renameCmd = &cobra.Command{
	Use:   "rename <new-name>",
	Short: "Rename the current session",
	Args:  cobra.ExactArgs(1),
	RunE:  runRename,
}

func runRename(cmd *cobra.Command, args []string) error {
	if !tmux.IsInsideTmux() {
		return fmt.Errorf("rename requires an active tmux session")
	}

	newName := args[0]
	if err := session.ValidateName(newName); err != nil {
		return err
	}

	out := output.Stderr()
	tc := tmux.NewClient(config.TmuxConfPath())
	store := session.NewStore(config.SessionsPath())

	oldName, err := tc.CurrentSession()
	if err != nil {
		return fmt.Errorf("getting current session: %w", err)
	}

	if err := tc.RenameSession(oldName, newName); err != nil {
		return fmt.Errorf("renaming tmux session: %w", err)
	}

	if err := store.Rename(oldName, newName); err != nil {
		out.Warn("state update failed: %v", err)
	}

	out.Success("renamed %q → %q", oldName, newName)
	return nil
}
