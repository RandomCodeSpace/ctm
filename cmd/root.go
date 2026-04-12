package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var Version = "0.1.0"

var Verbose bool

var rootCmd = &cobra.Command{
	Use:   "ctm [session-name]",
	Short: "Claude Tmux Manager — seamless session management",
	Long:  "ctm manages Claude Code sessions inside tmux with pre-flight health checks, persistent modes, and mobile-optimized configuration.",
	Args:  cobra.MaximumNArgs(1),
	// RunE will be set later by attach.go
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.CompletionOptions.HiddenDefaultCmd = true
	rootCmd.PersistentFlags().BoolVarP(&Verbose, "verbose", "v", false, "Show detailed debug output")
}
