package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// ClaudeJSONPath returns the canonical path to Claude Code's per-user config
// file (~/.claude.json). This file is owned by the Claude Code CLI — ctm
// only reads it, and writes only via EnsureRemoteControlAtStartup.
func ClaudeJSONPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude.json"), nil
}

// EnsureRemoteControlAtStartup sets "remoteControlAtStartup": true in
// ~/.claude.json so Claude Code's Remote Control feature is on by default
// for any new Claude session — including those spawned by ctm on a freshly-
// bootstrapped machine.
//
// Semantics (strictly conservative — the file is Claude Code's state, not
// ours):
//   - If the file does not exist, do nothing. Never create it.
//   - If the key is already present (true, false, or JSON null) we treat
//     that as a deliberate user choice and leave it alone. Only an absent
//     key triggers the write.
//   - Only when the key is absent do we write it true, preserving all other
//     keys via json.RawMessage round-trip (values byte-exact; top-level
//     key order becomes alphabetical — see patchJSONFile).
//
// The key `remoteControlAtStartup` was discovered empirically by toggling
// "Enable Remote Control for all sessions" in `/config` and diffing the
// resulting JSON. It is not a documented/stable API; if Claude Code renames
// it, future runs silently no-op (harmless).
//
// Errors are returned so callers can log; callers in ctm's boot path should
// swallow — remote-control defaults are convenience, not correctness, and
// must never block claude launch.
func EnsureRemoteControlAtStartup(path string) error {
	return patchJSONFile(path, func(obj map[string]json.RawMessage) bool {
		if _, present := obj["remoteControlAtStartup"]; present {
			return false
		}
		obj["remoteControlAtStartup"] = json.RawMessage("true")
		return true
	})
}
