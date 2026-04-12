package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

func init() {
	rootCmd.AddCommand(detachCmd)
}

var detachCmd = &cobra.Command{
	Use:   "detach",
	Short: "Detach from the current session",
	Args:  cobra.NoArgs,
	RunE:  runDetach,
}

func runDetach(cmd *cobra.Command, args []string) error {
	if !tmux.IsInsideTmux() {
		return fmt.Errorf("detach requires an active tmux session")
	}

	tc := tmux.NewClient(config.TmuxConfPath())
	return tc.Detach()
}
