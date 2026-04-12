package claude

import (
	"fmt"
	"os"
	"strings"
)

// OverlayPathIfExists returns overlayPath if the file exists and is readable,
// otherwise returns empty string. Used to gate the --settings flag.
func OverlayPathIfExists(overlayPath string) string {
	return pathIfExists(overlayPath)
}

// EnvFilePathIfExists returns envFilePath if the file exists and is readable,
// otherwise returns empty string. Used to gate env file sourcing.
func EnvFilePathIfExists(envFilePath string) string {
	return pathIfExists(envFilePath)
}

func pathIfExists(p string) string {
	if p == "" {
		return ""
	}
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes.
// This is safe for paths passed through /bin/sh -c.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// BuildCommand builds the claude CLI command string.
// If resume is true, tries --resume first, falls back to --session-id if the
// session no longer exists. Claude is the pane process — when it exits, the
// tmux session dies.
//
// If envFilePath is non-empty, it is sourced via `. 'path'` at exec time,
// BEFORE claude runs. This lets ctm set real shell env vars (e.g.
// CLAUDE_CODE_NO_FLICKER) that claude reads during early startup, which is
// too early for settings.json's `env` key to take effect.
//
// If overlayPath is non-empty, it is passed via --settings to layer ctm-only
// claude customizations (statusline, theme, etc.) on top of the user's global
// settings without modifying ~/.claude/settings.json. Both the env file check
// and the overlay check are TOCTOU-safe shell guards — `[ -r path ]` re-
// evaluates at exec time and falls back gracefully if the file vanished.
//
// NOTE: The || fallback fires on ANY non-zero exit from `claude --resume`,
// not just "session not found". A crash, auth error, or Ctrl-C will also
// trigger a fresh session with the same UUID. This is intentional — it's
// better to recover into a usable state than to leave the user stranded.
func BuildCommand(uuid, mode string, resume bool, overlayPath, envFilePath string) string {
	var dangerFlag string
	if mode == "yolo" {
		dangerFlag = " --dangerously-skip-permissions"
	}

	// claudeCmd returns a single claude invocation, with --settings 'path'
	// only if withOverlay is true.
	claudeCmd := func(sessionFlag string, withOverlay bool) string {
		base := fmt.Sprintf("claude %s %s%s", sessionFlag, uuid, dangerFlag)
		if withOverlay {
			base += " --settings " + shellQuote(overlayPath)
		}
		return base
	}

	// buildResume returns the resume-or-fresh fallback chain at one branch.
	buildResume := func(withOverlay bool) string {
		if !resume {
			return claudeCmd("--session-id", withOverlay)
		}
		return claudeCmd("--resume", withOverlay) + " || " + claudeCmd("--session-id", withOverlay)
	}

	// Core command: overlay-gated fallback chain.
	var core string
	if overlayPath == "" {
		core = buildResume(false)
	} else {
		// TOCTOU-safe: shell re-checks the overlay file at exec time.
		// Each branch is a complete invocation so paths with spaces stay as one arg.
		core = fmt.Sprintf("if [ -r %s ]; then %s; else %s; fi",
			shellQuote(overlayPath), buildResume(true), buildResume(false))
	}

	// Optional env-file prefix: source it at exec time if present.
	// Uses `.` (POSIX source) which works in bash/sh. The `{ ...; } || true`
	// wrapper ensures a sourcing error doesn't prevent claude from launching.
	if envFilePath != "" {
		return fmt.Sprintf("{ [ -r %s ] && . %s; }; %s",
			shellQuote(envFilePath), shellQuote(envFilePath), core)
	}
	return core
}
