package cmd

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/doctor"
	"github.com/RandomCodeSpace/ctm/internal/output"
	"github.com/RandomCodeSpace/ctm/internal/serve/auth"
	"github.com/RandomCodeSpace/ctm/internal/session"
	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

func init() {
	rootCmd.AddCommand(doctorCmd)
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Run full diagnostics: dependencies, config, sessions",
	Args:  cobra.NoArgs,
	RunE:  runDoctor,
}

// runDoctor is a thin CLI formatter over internal/doctor's shared
// probe primitives. The grouped-section output is preserved byte-for-
// byte from earlier releases (the V20 API endpoint uses the flat
// doctor.Run() slice form instead — same probes, different rendering).
func runDoctor(cmd *cobra.Command, args []string) error {
	out := output.Stdout()

	out.Bold("=== ctm doctor ===")
	fmt.Println()

	// Idempotently seed config.json, sessions.json, tmux.conf, and
	// serve.token. Safe on every invocation; this is the documented
	// remediation path when `ctm serve` reports a missing token.
	if _, err := ensureSetup(); err != nil {
		out.Warn("setup seeding failed: %v", err)
	}

	// --- Dependencies ---
	out.Bold("Dependencies:")
	for _, dep := range []string{"tmux", "claude", "git"} {
		if path, ok := doctor.LookupBinary(dep); ok {
			out.Success("  [OK] %-10s %s", dep, path)
		} else {
			out.Warn("  [MISSING] %s — not found in PATH", dep)
		}
	}
	fmt.Println()

	// --- Tmux version ---
	if v, ok := doctor.TmuxVersion(context.Background()); ok {
		out.Info("tmux version: %s", v)
	} else {
		out.Warn("tmux version: unavailable")
	}
	fmt.Println()

	// --- Config ---
	cfgPath := config.ConfigPath()
	out.Bold("Config: %s", cfgPath)
	cfg, err := config.Load(cfgPath)
	if err != nil {
		out.Warn("  could not load config: %v", err)
	} else {
		out.Info("  default_mode:               %s", cfg.DefaultMode)
		out.Info("  git_checkpoint_before_yolo: %v", cfg.GitCheckpointBeforeYolo)
		out.Info("  scrollback_lines:           %d", cfg.ScrollbackLines)
		out.Info("  health_check_timeout_sec:   %d", cfg.HealthCheckTimeoutSec)
		out.Info("  required_env:               %s", strings.Join(cfg.RequiredEnv, ", "))
		out.Info("  required_in_path:           %s", strings.Join(cfg.RequiredInPath, ", "))
	}
	fmt.Println()

	// --- Serve token ---
	tokenPath := auth.TokenPath()
	out.Bold("Serve token: %s", tokenPath)
	if info, err := os.Stat(tokenPath); err != nil {
		out.Warn("  missing — `ctm serve` will refuse to bind")
	} else {
		mode := info.Mode().Perm()
		if mode != 0o600 {
			out.Warn("  present but mode is %o (want 0600) — will refuse on strict checks", mode)
		} else {
			out.Success("  present (mode 0600, %d bytes)", info.Size())
		}
	}
	fmt.Println()

	// --- Sessions ---
	out.Bold("Sessions: %s", config.SessionsPath())
	store := session.NewStore(config.SessionsPath())
	tc := tmux.NewClient(config.TmuxConfPath())

	sessions, err := store.List()
	if err != nil {
		out.Warn("  could not list sessions: %v", err)
	} else if len(sessions) == 0 {
		out.Dim("  (no sessions)")
	} else {
		for _, sess := range sessions {
			tmuxStatus := "stopped"
			if tc.HasSession(sess.Name) {
				tmuxStatus = "running"
			}
			health := sess.LastHealthStatus
			if health == "" {
				health = "unknown"
			}
			out.Info("  %-20s  mode=%-6s  tmux=%-8s  health=%-8s  workdir=%s",
				sess.Name, sess.Mode, tmuxStatus, health, sess.Workdir)
		}
	}
	fmt.Println()

	out.Success("doctor complete")
	return nil
}
