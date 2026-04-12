package claude

import (
	"fmt"
	"os"
	"strings"
)

// OverlayPathIfExists returns overlayPath if the file exists and is readable,
// otherwise returns empty string. Used to gate the --settings flag.
func OverlayPathIfExists(overlayPath string) string {
	if overlayPath == "" {
		return ""
	}
	if _, err := os.Stat(overlayPath); err != nil {
		return ""
	}
	return overlayPath
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
// If overlayPath is non-empty, it is passed via --settings to layer ctm-only
// claude customizations (statusline, theme, etc.) on top of the user's global
// settings without modifying ~/.claude/settings.json. To avoid TOCTOU between
// ctm's existence check and tmux exec, an `if [ -r path ]` shell guard
// re-evaluates the file at exec time and falls back to no --settings if the
// file vanished. The if/else form (rather than command substitution) ensures
// the path is passed as a single argv entry even when it contains spaces.
//
// NOTE: The || fallback fires on ANY non-zero exit from `claude --resume`,
// not just "session not found". A crash, auth error, or Ctrl-C will also
// trigger a fresh session with the same UUID. This is intentional — it's
// better to recover into a usable state than to leave the user stranded.
// If claude ever exposes a distinct exit code for "session missing", this
// should be tightened to that specific code.
func BuildCommand(uuid, mode string, resume bool, overlayPath string) string {
	var dangerFlag string
	if mode == "yolo" {
		dangerFlag = " --dangerously-skip-permissions"
	}

	// withOverlay returns a single claude invocation, with --settings 'path'
	// only if the named flag is set.
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

	if overlayPath == "" {
		return buildResume(false)
	}

	// TOCTOU-safe: shell re-checks the overlay file at exec time.
	// Each branch is a complete invocation so paths with spaces stay as one arg.
	return fmt.Sprintf("if [ -r %s ]; then %s; else %s; fi",
		shellQuote(overlayPath), buildResume(true), buildResume(false))
}
