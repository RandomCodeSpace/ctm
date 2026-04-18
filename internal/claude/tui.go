package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// SettingsJSONPath returns the canonical path to Claude Code's user-level
// settings file (~/.claude/settings.json). Unlike ~/.claude.json (which
// stores per-user runtime state), this file holds the documented
// user-overridable configuration.
func SettingsJSONPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// EnsureTUIFullscreen pins "tui": "fullscreen" in Claude Code's settings.json
// when the key is absent OR explicitly set to "default". Any other value is
// treated as an intentional user choice and left untouched.
//
// Rationale: at the time of writing, Claude Code's "default" renderer IS the
// fullscreen renderer — `/tui fullscreen` reports "Already using the
// fullscreen renderer" when the setting is "default". Pinning to
// "fullscreen" is a forward-looking hedge so new machines keep the
// fullscreen UI even if Claude Code later redefines what "default" means.
//
// Semantics:
//   - Missing settings.json → no-op. Never create it.
//   - Invalid JSON → return error; never clobber the user's file.
//   - Absent key OR value == "default" → write "fullscreen".
//   - Any other value (including JSON null, "compact", or a custom mode)
//     is respected as an explicit user choice and left alone.
//   - Atomic write via temp + rename; preserves original file mode.
//
// Errors are returned so callers can log; ctm's boot path swallows them.
func EnsureTUIFullscreen(path string) error {
	return patchJSONFile(path, func(obj map[string]json.RawMessage) bool {
		cur, present := obj["tui"]
		if present && string(cur) != `"default"` {
			return false
		}
		obj["tui"] = json.RawMessage(`"fullscreen"`)
		return true
	})
}

// EnsureViewModeFocus pins "viewMode": "focus" in Claude Code's
// settings.json when the key is absent OR explicitly set to "default".
// Any other value ("verbose", or a future mode) is treated as an
// intentional user choice and left alone.
//
// Context: `viewMode` is the documented startup default for the transcript
// view (default | verbose | focus). Setting it to "focus" yields the
// streamlined "last prompt + tool-call summaries + final response" layout
// that is otherwise toggled at runtime via `/focus`. This pairs with
// EnsureTUIFullscreen — the focus view only renders under the fullscreen
// TUI, so pinning both makes the intended mobile-first view the default.
//
// Semantics mirror EnsureTUIFullscreen:
//   - Missing settings.json → no-op. Never create it.
//   - Invalid JSON → return error.
//   - Absent key OR value == "default" → write "focus".
//   - Any other value is respected as an explicit user choice.
//   - Atomic write; preserves file mode.
//
// Reference: https://code.claude.com/docs/en/settings (viewMode).
func EnsureViewModeFocus(path string) error {
	return patchJSONFile(path, func(obj map[string]json.RawMessage) bool {
		cur, present := obj["viewMode"]
		if present && string(cur) != `"default"` {
			return false
		}
		obj["viewMode"] = json.RawMessage(`"focus"`)
		return true
	})
}
