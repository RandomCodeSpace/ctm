package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/session"
	"github.com/RandomCodeSpace/ctm/internal/shell"
)

func init() {
	rootCmd.AddCommand(forgetCmd)
}

var forgetCmd = &cobra.Command{
	Use:               "forget <name>",
	Short:             "Clear saved conversation mapping for a session",
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: shell.SessionNameCompletion(),
	RunE:              runForget,
}

func runForget(cmd *cobra.Command, args []string) error {
	name := args[0]
	store := session.NewStore(config.SessionsPath())

	if err := store.Delete(name); err != nil {
		return fmt.Errorf("session %q not found: %w", name, err)
	}

	fmt.Printf("forgot session %q\n", name)
	return nil
}
