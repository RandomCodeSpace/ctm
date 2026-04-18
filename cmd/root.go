package cmd

import (
	"fmt"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/RandomCodeSpace/ctm/internal/logging"
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

var (
	Verbose  bool
	LogLevel string
)

var rootCmd = &cobra.Command{
	Use:   "ctm [session-name]",
	Short: "Claude Tmux Manager — seamless session management",
	Long:  "ctm manages Claude Code sessions inside tmux with pre-flight health checks, persistent modes, and mobile-optimized configuration.",
	Args:  cobra.MaximumNArgs(1),
	// Configure slog before any subcommand runs so diagnostic lines
	// from pre-flight checks and state loads respect --log-level.
	// --verbose is a legacy alias for --log-level=debug.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		lvl := LogLevel
		if Verbose && lvl == "" {
			lvl = "debug"
		}
		if err := logging.Setup(lvl); err != nil {
			return fmt.Errorf("invalid --log-level: %w", err)
		}
		return nil
	},
	// RunE will be set later by attach.go
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	// Disable the cobra-auto-generated `completion` so our explicit
	// one in cmd/completion.go owns the subcommand namespace and can
	// carry install instructions in its Long help.
	rootCmd.CompletionOptions.DisableDefaultCmd = true
	rootCmd.PersistentFlags().BoolVarP(&Verbose, "verbose", "v", false, "Show detailed debug output (alias for --log-level=debug)")
	rootCmd.PersistentFlags().StringVar(&LogLevel, "log-level", "", "Structured diagnostic log level (debug|info|warn|error) — writes to stderr. Default: info.")
}
