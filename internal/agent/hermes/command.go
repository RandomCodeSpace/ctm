package hermes

import (
	"fmt"
	"strings"
)

// shellQuote wraps s in single quotes, escaping any embedded single quotes.
// Safe for paths and IDs passed through /bin/sh -c.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// BuildCommand builds the hermes CLI command string.
//
// agentSessionID is the hermes session ID (Session.AgentSessionID). On
// resume:
//   - if non-empty: `hermes --tui --resume <id>`
//   - if empty:     `hermes --tui -c` (continue most-recent)
//
// In both resume branches the command falls back to a fresh `hermes --tui`
// on non-zero exit — matches codex's pattern for crash/Ctrl-C parity.
// (Hermes itself exits 0 even on bad ID, so this fallback is purely
// defensive against crashes during resume.)
//
// --tui is always passed: ctm only ever drives hermes through its TUI
// surface.
//
// envExports, when non-empty, is prepended verbatim as a shell prelude.
func BuildCommand(agentSessionID, mode string, resume bool, envExports string) string {
	var yoloFlag string
	if mode == "yolo" {
		yoloFlag = " --yolo"
	}

	freshCmd := "hermes --tui" + yoloFlag

	var core string
	switch {
	case !resume:
		core = freshCmd
	case agentSessionID != "":
		core = fmt.Sprintf("hermes --tui --resume %s%s || %s",
			shellQuote(agentSessionID), yoloFlag, freshCmd)
	default:
		core = "hermes --tui -c" + yoloFlag + " || " + freshCmd
	}

	if envExports != "" {
		return envExports + "; " + core
	}
	return core
}
