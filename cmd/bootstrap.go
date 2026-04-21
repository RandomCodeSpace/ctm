package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RandomCodeSpace/ctm/internal/claude"
	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/logrotate"
	"github.com/RandomCodeSpace/ctm/internal/migrate"
	"github.com/RandomCodeSpace/ctm/internal/serve/auth"
	"github.com/RandomCodeSpace/ctm/internal/session"
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
	// Run schema migrations before typed Load so the unmarshal sees a
	// file already at the current version. Missing files are a no-op —
	// the migrator never creates them.
	if err := runStateMigrations(); err != nil {
		return nil, fmt.Errorf("migrating state files: %w", err)
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
	_ = ensureClaudeRemoteControlDefault()
	_ = ensureClaudeTUIFullscreenDefault()
	_ = ensureClaudeViewModeFocusDefault()
	_ = ensureServeToken()
	_ = pruneSessionLogs(cfg)
	return &cfg, nil
}

// ensureServeToken seeds ~/.config/ctm/serve.token (mode 0600) on first
// run so the auth-protected `ctm serve` daemon has a stable bearer to
// validate against. Idempotent: existing tokens are read and returned
// unchanged. Errors are swallowed in line with the rest of ensureSetup
// — token absence will surface as a clear startup error from
// `ctm serve` itself.
func ensureServeToken() error {
	_, err := auth.EnsureToken(auth.TokenPath())
	return err
}

// pruneSessionLogs walks the session log directory and applies the
// user's retention policy to every set of rotated siblings. This catches
// abandoned sessions whose active log no longer writes and therefore
// never triggers an inline MaybeRotate. Errors are swallowed — pruning
// is hygiene, not correctness, and must not block startup.
//
// The walk is bounded by the number of active sessions (typically small
// for a personal ctm user), so the cost is negligible on every run.
func pruneSessionLogs(cfg config.Config) error {
	dir := filepath.Join(config.Dir(), "logs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil // logs dir absent → nothing to prune
	}
	policy := cfg.LogPolicy()
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		_ = logrotate.Prune(filepath.Join(dir, e.Name()), policy)
	}
	return nil
}

// runStateMigrations applies the pending migration Plan for each ctm-owned
// JSON state file. Missing files are a no-op. Each file that actually
// migrates gets a timestamped ".bak.<unix-nano>" sibling before any write,
// so the user can recover by hand if a later step reveals a problem.
//
// Errors here are fatal to bootstrap — if we cannot read or migrate state,
// subsequent commands will mis-interpret or clobber it. Better to refuse
// to start than to corrupt the only copy of the user's sessions.
func runStateMigrations() error {
	plans := []struct {
		path string
		plan migrate.Plan
	}{
		{config.ConfigPath(), config.MigrationPlan()},
		{config.SessionsPath(), session.MigrationPlan()},
	}
	for _, p := range plans {
		if _, err := migrate.Run(p.path, p.plan); err != nil {
			return fmt.Errorf("%s: %w", p.path, err)
		}
	}
	return nil
}

// ensureClaudeRemoteControlDefault opts new Claude Code installs into Remote
// Control by default. Never creates ~/.claude.json, never overwrites an
// explicit user choice (true or false) — only fills in the key when it is
// absent. See internal/claude.EnsureRemoteControlAtStartup for the full
// contract. Errors are swallowed; this is convenience, not correctness.
func ensureClaudeRemoteControlDefault() error {
	path, err := claude.ClaudeJSONPath()
	if err != nil {
		return err
	}
	return claude.EnsureRemoteControlAtStartup(path)
}

// ensureClaudeTUIFullscreenDefault pins Claude Code's TUI renderer to
// "fullscreen" in ~/.claude/settings.json when the key is absent or set to
// "default". Any other explicit value (e.g., "compact") is treated as a
// deliberate user choice and left alone. See
// internal/claude.EnsureTUIFullscreen for the full contract.
func ensureClaudeTUIFullscreenDefault() error {
	path, err := claude.SettingsJSONPath()
	if err != nil {
		return err
	}
	return claude.EnsureTUIFullscreen(path)
}

// ensureClaudeViewModeFocusDefault pins Claude Code's default transcript
// view mode to "focus" in ~/.claude/settings.json when the key is absent
// or set to "default". Any other explicit value ("verbose" or a future
// mode) is treated as a deliberate user choice and left alone. See
// internal/claude.EnsureViewModeFocus for the full contract.
//
// Pairs with ensureClaudeTUIFullscreenDefault — focus view only renders
// under the fullscreen TUI, so we set both together to land on the
// mobile-first default ctm is optimised for.
func ensureClaudeViewModeFocusDefault() error {
	path, err := claude.SettingsJSONPath()
	if err != nil {
		return err
	}
	return claude.EnsureViewModeFocus(path)
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
	f, err := os.OpenFile(overlay, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
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
