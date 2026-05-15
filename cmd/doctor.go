package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/RandomCodeSpace/ctm/internal/agent"
	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/doctor"
	"github.com/RandomCodeSpace/ctm/internal/output"
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

	// Idempotently seed config.json, sessions.json, and tmux.conf.
	if _, err := ensureSetup(); err != nil {
		out.Warn("setup seeding failed: %v", err)
	}

	// --- Dependencies ---
	out.Bold("Dependencies:")
	deps := append([]string{"tmux"}, agent.Registered()...)
	deps = append(deps, "git")
	for _, dep := range deps {
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
