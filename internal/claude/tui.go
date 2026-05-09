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
//
// ctm reads this file (e.g., for the "effortLevel" key consumed by the
// statusline renderer) but never writes to it. UI-shaping defaults
// (tui, viewMode) live in claude-overlay.json — see buildSampleOverlay
// in cmd/overlay.go — so ctm stays additive, not invasive, on the
// user's per-user Claude Code config.
func SettingsJSONPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

// ReadEffortLevel returns the current effort level stored under the
// "effortLevel" key in path (typically ~/.claude/settings.json). Values
// in the wild: "min" / "low" / "medium" / "high" / "xhigh" / "max".
//
// Returns "" when the file is missing, unreadable, unparseable, the key
// is absent, or the value is not a string — intentionally silent so
// callers (the statusline renderer) can display nothing on missing data
// without a noisy error path.
func ReadEffortLevel(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return ""
	}
	raw, ok := obj["effortLevel"]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return s
}
