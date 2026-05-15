package codex

import (
	"fmt"
	"strings"
)

// shellQuote wraps s in single quotes, escaping any embedded single quotes.
// Safe for paths and IDs passed through /bin/sh -c.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// BuildCommand builds the codex CLI command string.
//
// agentSessionID is the codex thread UUID (Session.AgentSessionID). On
// resume:
//   - if non-empty: `codex resume <id>` (positional)
//   - if empty: `codex resume --last`
//
// In both resume branches the command falls back to a fresh `codex` on
// non-zero exit — better to land in a usable state than strand the user
// when the prior session can't be resumed. Crashes, auth failures, or
// Ctrl-C will also trigger the fallback; that's intentional.
//
// envExports, when non-empty, is prepended verbatim as a shell prelude.
// It comes from <agent>-env.json via the caller — for codex this lets
// the user set provider keys or PATH bits that need to be visible to
// codex's early startup, too early for codex's own config.toml shell
// policy to take effect.
func BuildCommand(agentSessionID, mode string, resume bool, envExports string) string {
	var sandboxFlag string
	if mode == "yolo" {
		sandboxFlag = " --sandbox danger-full-access"
	}

	freshCmd := "codex" + sandboxFlag

	var core string
	switch {
	case !resume:
		core = freshCmd
	case agentSessionID != "":
		core = fmt.Sprintf("codex resume %s%s || %s",
			shellQuote(agentSessionID), sandboxFlag, freshCmd)
	default:
		core = "codex resume --last" + sandboxFlag + " || " + freshCmd
	}

	if envExports != "" {
		return envExports + "; " + core
	}
	return core
}
