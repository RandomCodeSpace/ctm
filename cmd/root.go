package cmd

import (
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// Version is the ctm version string. It is resolved at runtime from Go's
// build info when installed via `go install github.com/RandomCodeSpace/ctm@vX.Y.Z`
// (Go module metadata carries the tag). For local `go build` from source,
// debug.ReadBuildInfo returns "(devel)" and we fall back to "dev".
//
// This variable is also overridable at build time via ldflags for custom
// distributions:
//
//	go build -ldflags "-X github.com/RandomCodeSpace/ctm/cmd.Version=v1.2.3"
var Version = resolveVersion()

func resolveVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	v := info.Main.Version
	if v == "" || v == "(devel)" {
		return "dev"
	}
	return v
}

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
