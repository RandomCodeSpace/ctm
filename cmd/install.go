package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/output"
	"github.com/RandomCodeSpace/ctm/internal/shell"
	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

var installCmd = &cobra.Command{
	Use:   "install",
	Short: "Set up ctm shell integration",
	Long:  "Creates config directory, generates default config and tmux.conf, and injects aliases into shell rc files.",
	RunE:  runInstall,
}

var uninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Remove ctm shell integration",
	Long:  "Removes aliases from shell rc files and deletes the ctm config directory.",
	RunE:  runUninstall,
}

func init() {
	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(uninstallCmd)
}

func runInstall(cmd *cobra.Command, args []string) error {
	out := output.Stdout()

	// 1. Create config directory
	cfgDir := config.Dir()
	if err := os.MkdirAll(cfgDir, 0755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	out.Success("Config directory: %s", cfgDir)

	// 2. Generate default config (creates if missing)
	cfg, err := config.Load(config.ConfigPath())
	if err != nil {
		return fmt.Errorf("generate config: %w", err)
	}
	out.Success("Config file: %s", config.ConfigPath())

	// 3. Generate tmux.conf
	if err := tmux.GenerateConfig(config.TmuxConfPath(), cfg.ScrollbackLines); err != nil {
		return fmt.Errorf("generate tmux config: %w", err)
	}
	out.Success("tmux config: %s", config.TmuxConfPath())

	// 4. Inject aliases into ~/.bashrc
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	bashrc := filepath.Join(home, ".bashrc")
	if err := shell.InjectAliases(bashrc); err != nil {
		out.Warn("Could not inject aliases into %s: %v", bashrc, err)
	} else {
		out.Success("Aliases injected: %s", bashrc)
	}

	// Also try ~/.zshrc if it exists
	zshrc := filepath.Join(home, ".zshrc")
	if _, err := os.Stat(zshrc); err == nil {
		if err := shell.InjectAliases(zshrc); err != nil {
			out.Warn("Could not inject aliases into %s: %v", zshrc, err)
		} else {
			out.Success("Aliases injected: %s", zshrc)
		}
	}

	// 5. Migrate from cc if ~/.claude/cc-sessions exists
	ccSessionsDir := filepath.Join(home, ".claude", "cc-sessions")
	if _, err := os.Stat(ccSessionsDir); err == nil {
		migrated, err := shell.MigrateFromCC(ccSessionsDir, config.SessionsPath())
		if err != nil {
			out.Warn("Migration warning: %v", err)
		} else if len(migrated) > 0 {
			out.Success("Migrated %d session(s) from cc: %v", len(migrated), migrated)
		} else {
			out.Info("No sessions to migrate from cc.")
		}
	}

	// 6. Claude-side defaults (idempotent, conservative).
	//
	// These mirror ensureSetup()'s claude-side bootstrap so `ctm install`
	// is a full explicit setup, not just a partial one. Each helper is
	// strictly no-op when the relevant key is absent or explicitly
	// "default"; any other user value is respected. See
	// internal/claude.{EnsureRemoteControlAtStartup,EnsureTUIFullscreen,
	// EnsureViewModeFocus} for the per-key contracts.
	if err := ensureClaudeRemoteControlDefault(); err == nil {
		out.Success("Claude remote control: default on (~/.claude.json)")
	}
	if err := ensureClaudeTUIFullscreenDefault(); err == nil {
		out.Success("Claude TUI: fullscreen (~/.claude/settings.json)")
	}
	if err := ensureClaudeViewModeFocusDefault(); err == nil {
		out.Success("Claude viewMode: focus (~/.claude/settings.json)")
	}

	// 7. Print summary
	fmt.Println()
	out.Bold("ctm installed successfully!")
	out.Info("Run:  source ~/.bashrc")
	out.Info("Then: ctm --help")

	return nil
}

func runUninstall(cmd *cobra.Command, args []string) error {
	out := output.Stdout()

	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("get home dir: %w", err)
	}

	// 1. Remove aliases from ~/.bashrc and ~/.zshrc
	for _, rc := range []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".zshrc"),
	} {
		if err := shell.RemoveAliases(rc); err != nil {
			out.Warn("Could not remove aliases from %s: %v", rc, err)
		} else {
			out.Success("Aliases removed: %s", rc)
		}
	}

	// 2. Remove config directory
	cfgDir := config.Dir()
	if err := os.RemoveAll(cfgDir); err != nil {
		return fmt.Errorf("remove config dir: %w", err)
	}
	out.Success("Config directory removed: %s", cfgDir)

	// 3. Print notes
	fmt.Println()
	out.Warn("Note: the ctm binary was not removed.")
	out.Warn("Note: session state has been removed.")
	out.Warn("Note: existing tmux sessions are unaffected.")

	return nil
}
