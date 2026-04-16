package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/shell"
	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

// ensureSetup runs the idempotent first-run bootstrap. Safe to call on
// every claude-launching command. Returns the (possibly freshly-created)
// config. Errors from non-critical steps (overlay, aliases) are swallowed
// — they must never block launching claude on a well-configured host.
//
// Side effects (all idempotent):
//   - creates ~/.config/ctm/ if missing
//   - writes config.json with defaults if missing
//   - regenerates tmux.conf on every call so new defaults reach upgraded users
//   - writes claude-overlay.json + env.sh + logs/ dir if missing
//   - injects shell aliases into ~/.bashrc and ~/.zshrc if markers not present
func ensureSetup() (*config.Config, error) {
	if err := os.MkdirAll(config.Dir(), 0755); err != nil {
		return nil, fmt.Errorf("creating config dir: %w", err)
	}
	cfg, err := config.Load(config.ConfigPath())
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	if err := tmux.GenerateConfig(config.TmuxConfPath(), cfg.ScrollbackLines); err != nil {
		return nil, fmt.Errorf("generating tmux config: %w", err)
	}
	_ = ensureOverlaySidecars()
	_ = ensureAliases()
	return &cfg, nil
}

// ensureOverlaySidecars writes claude-overlay.json, env.sh, and the
// per-session logs dir if any are missing. Leaves existing files alone —
// user edits to overlay/env always win.
func ensureOverlaySidecars() error {
	_ = os.MkdirAll(sessionLogDir(), 0755)
	_ = writeEnvFile(config.EnvFilePath())

	overlay := config.ClaudeOverlayPath()
	if _, err := os.Stat(overlay); err == nil {
		return nil
	}
	f, err := os.OpenFile(overlay, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()
	_, err = f.WriteString(buildSampleOverlay(statuslineHookCommand(), logToolUseHookCommand()))
	return err
}

// ensureAliases injects the ctm alias block into ~/.bashrc and ~/.zshrc
// when the start marker is absent. Avoids rewriting the file on every
// ctm invocation — InjectAliases is idempotent but always touches the
// file, which would update mtime and trigger rc reloaders unnecessarily.
func ensureAliases() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	for _, rc := range []string{
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".zshrc"),
	} {
		data, err := os.ReadFile(rc)
		if err != nil {
			continue // don't create rc files that aren't there
		}
		if strings.Contains(string(data), "# --- ctm aliases START ---") {
			continue
		}
		_ = shell.InjectAliases(rc)
	}
	return nil
}
