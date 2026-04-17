package claude

import (
	"encoding/json"
	"fmt"
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
// ~/.claude.json so Claude Code's Remote Control feature is on by default for
// any new Claude session — including those spawned by ctm on a freshly-
// bootstrapped machine.
//
// Semantics (strictly conservative — the file is Claude Code's state, not ours):
//   - If the file does not exist, do nothing. Never create it; Claude Code
//     owns the lifecycle of ~/.claude.json.
//   - If the key is already present (true OR false), do nothing. This
//     respects an explicit user choice made via `/config`, including
//     opt-out. "missing" and "false" are deliberately treated differently.
//   - Only when the key is absent do we write it true, preserving all other
//     keys byte-for-byte via json.RawMessage round-trip.
//   - Writes are atomic (temp file + rename) and preserve the original file
//     mode (typically 0600).
//
// The key `remoteControlAtStartup` was discovered empirically by toggling
// "Enable Remote Control for all sessions" in `/config` and diffing the
// resulting JSON. It is not a documented/stable API; if Claude Code renames
// it, future runs of this function silently no-op (the old key would be
// absent, we'd add our own, Claude Code would ignore it — harmless).
//
// Concurrency caveat: if Claude Code writes to ~/.claude.json between our
// Read and our Rename, that write is lost. In practice ctm bootstrap runs
// before `claude` launches, so the race window is small and the impact
// (losing one Claude Code-authored update) is acceptable for this best-
// effort convenience feature. No flock to keep the bootstrap fast.
//
// Errors are returned so the caller can log; callers in ctm's boot path
// should swallow — remote-control defaults are convenience, not correctness,
// and must never block claude launch.
func EnsureRemoteControlAtStartup(path string) error {
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

	if _, present := obj["remoteControlAtStartup"]; present {
		return nil
	}

	obj["remoteControlAtStartup"] = json.RawMessage("true")

	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling %s: %w", path, err)
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".claude.json.*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmp.Name()
	// best-effort cleanup if we bail before rename
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
