package claude

import (
	"encoding/json"
	"fmt"
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
// fullscreen renderer" when the setting is "default". Pinning to "fullscreen"
// is a forward-looking hedge so new machines keep the fullscreen UI even if
// Claude Code later redefines what "default" means.
//
// Semantics (parallel to EnsureRemoteControlAtStartup):
//   - Missing settings.json → no-op. Never create it; Claude Code owns the
//     file's lifecycle.
//   - Invalid JSON → return error; never clobber the user's file.
//   - Atomic write via temp + rename; preserves the original file mode.
//   - Concurrency caveat: a concurrent Claude Code write between our Read
//     and Rename is lost. Acceptable for a best-effort convenience default
//     that runs before `claude` launches.
//
// Errors are returned so callers can log; ctm's boot path swallows them.
func EnsureTUIFullscreen(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}

	cur, present := obj["tui"]
	if present && string(cur) != `"default"` {
		return nil
	}

	obj["tui"] = json.RawMessage(`"fullscreen"`)

	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling %s: %w", path, err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "settings.json.*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) //nolint:errcheck

	if _, err := tmp.Write(out); err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("writing temp file: %w", err)
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("renaming temp file: %w", err)
	}
	return nil
}
