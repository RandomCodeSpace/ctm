package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/output"
	"github.com/spf13/cobra"
)

// Repeated overlay messages / format strings extracted to satisfy the
// no-duplicate-literal rule.
const (
	errCreatingConfigDirFmt = "creating config dir: %w"
	dimStatusLineFmt        = "statusLine: %s"
	dimEnvFileFmt           = "env file: %s"
)

func init() {
	overlayCmd.AddCommand(overlayInitCmd)
	overlayCmd.AddCommand(overlayEditCmd)
	overlayCmd.AddCommand(overlayPathCmd)
	rootCmd.AddCommand(overlayCmd)
}

var overlayCmd = &cobra.Command{
	Use:   "overlay",
	Short: "Manage the ctm-only claude settings overlay",
	Long: "The overlay file at ~/.config/ctm/claude-overlay.json contains claude " +
		"settings (statusline, theme, etc.) that apply ONLY when claude is launched " +
		"via ctm. Direct `claude` invocations are unaffected.",
	RunE: runOverlayStatus,
}

var overlayInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a sample overlay file with statusline + theme examples",
	RunE:  runOverlayInit,
}

var overlayEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Open the overlay file in $EDITOR (creates it first if missing)",
	RunE:  runOverlayEdit,
}

var overlayPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the overlay file path",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(config.ClaudeOverlayPath())
		return nil
	},
}

// sessionLogDir returns the directory where per-session tool-use logs are written.
func sessionLogDir() string {
	return filepath.Join(config.Dir(), "logs")
}

// ctmSubcommand returns the absolute path to the ctm binary plus the given
// subcommand, suitable for embedding in claude hook JSON. We resolve at
// overlay generation time so hooks keep working even if PATH changes.
// Falls back to bare "ctm <sub>" if os.Executable fails, which is rare.
func ctmSubcommand(sub string) string {
	if exe, err := os.Executable(); err == nil && exe != "" {
		return exe + " " + sub
	}
	return "ctm " + sub
}

// logToolUseHookCommand is the PostToolUse hook target (hidden subcommand
// in cmd/log_tool_use.go). Pure Go — no jq / bash dependency.
func logToolUseHookCommand() string { return ctmSubcommand("log-tool-use") }

// statuslineHookCommand is the statusLine.command target (hidden subcommand
// in cmd/statusline.go). Pure Go — no jq / awk / bash dependency.
func statuslineHookCommand() string { return ctmSubcommand("statusline") }

// buildSampleOverlay returns the overlay JSON, pointing statusLine at the
// built-in `ctm statusline` renderer and PostToolUse at the logging hook.
// Both hook commands are resolved to the ctm binary at write time so they
// keep working even if PATH changes.
//
// `tui`, `viewMode`, and `remoteControlAtStartup` live here (not in
// ~/.claude/settings.json or ~/.claude.json) so ctm never mutates any
// Claude-owned config file on disk. The overlay is merged on top of
// settings.json only when claude is launched via ctm — direct `claude`
// invocations are completely unaffected by ctm's defaults.
//
// Note: env vars like CLAUDE_CODE_NO_FLICKER cannot go here — claude
// reads them too early in startup for settings.json's env key to take
// effect. They live in ~/.config/ctm/claude-env.json (see
// sampleClaudeEnvJSON) and are exported by the shell before claude
// launches via config.ClaudeEnvExports().
func buildSampleOverlay(statuslineCmd, logHookCmd string) string {
	return fmt.Sprintf(`{
  "reduceMotion": false,
  "spinnerTipsEnabled": false,
  "statusLine": {
    "type": "command",
    "command": %q
  },
  "theme": "dark",
  "tui": "fullscreen",
  "viewMode": "focus",
  "remoteControlAtStartup": true,
  "env": {
    "CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": "1"
  },
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": %q
          }
        ]
      }
    ]
  }
}
`, statuslineCmd, logHookCmd)
}

// sampleClaudeEnvJSON is the JSON env file ctm reads at every claude
// launch and exports into the shell BEFORE exec'ing claude. Use this
// for env vars claude reads during CLI startup, which is too early for
// the overlay's `env` block to take effect. Most env vars (including
// CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS) belong in claude-overlay.json's
// `env` block instead and should be put there.
//
// Pre-seeded with the two ctm-default vars:
//   - CLAUDE_CODE_NO_FLICKER: flicker-free streaming markdown rendering
//   - CTM_STATUSLINE_DUMP:    where `ctm statusline` writes per-session
//                             quota dumps; `{uuid}` is substituted by
//                             the statusline subcommand at render time.
const sampleClaudeEnvJSON = `{
  "_comment": "ctm-managed env vars exported into the shell that spawns claude. Only affects claude processes launched via ctm; direct 'claude' calls outside ctm are unaffected. Use this for vars claude reads too early in startup for claude-overlay.json's 'env' block to take effect. For anything else, prefer the overlay's 'env' block.",
  "env": {
    "CLAUDE_CODE_NO_FLICKER": "1",
    "CTM_STATUSLINE_DUMP": "/tmp/ctm-statusline/{uuid}.json"
  }
}
`

// writeClaudeEnv writes the default claude-env.json to path, creating
// parent dirs. Uses O_EXCL so parallel invocations don't clobber each
// other, and leaves an existing file untouched (so user edits survive).
func writeClaudeEnv(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf(errCreatingConfigDirFmt, err)
	}
	// 0600: claude-env.json is exported by the shell that spawns claude
	// and is a natural place for users to park secrets (API keys,
	// tokens). Default to owner-only so a user who drops a secret in
	// doesn't leak it to other users on a shared host.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if os.IsExist(err) {
			return nil // keep user edits intact
		}
		return fmt.Errorf("creating claude-env.json: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(sampleClaudeEnvJSON); err != nil {
		return fmt.Errorf("writing claude-env.json: %w", err)
	}
	return nil
}

func runOverlayStatus(cmd *cobra.Command, args []string) error {
	out := output.Stdout()
	path := config.ClaudeOverlayPath()
	if _, err := os.Stat(path); err == nil {
		out.Success("overlay active: %s", path)
		out.Dim(dimStatusLineFmt, statuslineHookCommand())
		out.Dim("PostToolUse: %s", logToolUseHookCommand())
		envPath := config.ClaudeEnvPath()
		if _, err := os.Stat(envPath); err == nil {
			out.Dim(dimEnvFileFmt, envPath)
		}
	} else {
		out.Dim("no overlay file at %s", path)
		out.Dim("create one with: ctm overlay init")
	}
	return nil
}

func runOverlayInit(cmd *cobra.Command, args []string) error {
	out := output.Stdout()
	path := config.ClaudeOverlayPath()
	envPath := config.ClaudeEnvPath()
	slCmd := statuslineHookCommand()
	logCmd := logToolUseHookCommand()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf(errCreatingConfigDirFmt, err)
	}
	if err := writeClaudeEnv(envPath); err != nil {
		return err
	}
	if err := os.MkdirAll(sessionLogDir(), 0755); err != nil {
		return fmt.Errorf("creating session log dir: %w", err)
	}

	// O_EXCL is atomic against concurrent creators — no TOCTOU race.
	// 0600: personal claude config under ~/.config/ctm/ (0700 dir); no need
	// to be world- or group-readable.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("overlay already exists at %s — delete it first or use `ctm overlay edit`", path)
		}
		return fmt.Errorf("creating overlay: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(buildSampleOverlay(slCmd, logCmd)); err != nil {
		return fmt.Errorf("writing overlay: %w", err)
	}

	out.Success("created %s", path)
	out.Dim(dimEnvFileFmt, envPath)
	out.Dim(dimStatusLineFmt, slCmd)
	out.Dim("PostToolUse hook: %s", logCmd)
	out.Dim("session logs dir: %s (view: ctm logs)", sessionLogDir())
	out.Dim("edit with: ctm overlay edit")
	out.Dim("applies to all NEW ctm sessions; restart existing ones to pick up changes")
	return nil
}

func runOverlayEdit(cmd *cobra.Command, args []string) error {
	out := output.Stdout()
	path := config.ClaudeOverlayPath()
	envPath := config.ClaudeEnvPath()
	slCmd := statuslineHookCommand()
	logCmd := logToolUseHookCommand()

	// Resolve editor BEFORE touching the filesystem so a missing $EDITOR
	// doesn't leave a half-created sample file behind.
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	editorBin, err := exec.LookPath(editor)
	if err != nil {
		return fmt.Errorf("editor %q not found in PATH — set $EDITOR or install it; overlay path: %s", editor, path)
	}

	// Create with sample if missing, atomically.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf(errCreatingConfigDirFmt, err)
		}
		if err := writeClaudeEnv(envPath); err != nil {
			return err
		}
		_ = os.MkdirAll(sessionLogDir(), 0755)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil && !os.IsExist(err) {
			return fmt.Errorf("creating overlay: %w", err)
		}
		if err == nil {
			if _, werr := f.WriteString(buildSampleOverlay(slCmd, logCmd)); werr != nil {
				f.Close()
				return fmt.Errorf("writing overlay: %w", werr)
			}
			f.Close()
			out.Dim("created sample overlay at %s", path)
			out.Dim(dimEnvFileFmt, envPath)
			out.Dim(dimStatusLineFmt, slCmd)
			out.Dim("PostToolUse hook: %s", logCmd)
		}
	}

	c := exec.Command(editorBin, path)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
