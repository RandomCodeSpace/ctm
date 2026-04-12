package claude

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// IsClaudeAlive checks if a PID exists and is not a zombie.
func IsClaudeAlive(pid string) (bool, error) {
	if pid == "" {
		return false, errors.New("pid must not be empty")
	}

	// Validate it's a parseable integer
	var pidInt int
	if _, err := fmt.Sscanf(pid, "%d", &pidInt); err != nil || pidInt <= 0 {
		return false, fmt.Errorf("invalid pid: %q", pid)
	}

	statusPath := fmt.Sprintf("/proc/%s/status", pid)
	f, err := os.Open(statusPath)
	if err != nil {
		// File doesn't exist → process not alive
		return false, nil
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "State:") {
			if strings.Contains(line, "Z (zombie)") {
				return false, nil
			}
			return true, nil
		}
	}

	return true, nil
}

// FindClaudeChild finds a claude process among children of the given PID by
// walking /proc/*/status. Pure Go — no pgrep dependency.
//
// For each PID directory under /proc, it reads /proc/<pid>/status and checks
// the PPid and Name fields. Returns the first PID whose PPid == panePID and
// Name == "claude", or empty string if none found.
func FindClaudeChild(panePID string) string {
	if panePID == "" {
		return ""
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return ""
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Only numeric entries are PID directories.
		name := e.Name()
		if !isNumeric(name) {
			continue
		}

		ppid, procName, ok := readProcStatus("/proc/" + name + "/status")
		if !ok {
			continue
		}
		if ppid == panePID && procName == "claude" {
			return name
		}
	}

	return ""
}

// readProcStatus parses /proc/<pid>/status and returns (PPid, Name, ok).
// ok is false on any read/parse error or if the file doesn't contain both
// fields. Returns as soon as both fields are seen to avoid reading the rest.
func readProcStatus(path string) (ppid, procName string, ok bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "Name:"):
			procName = strings.TrimSpace(strings.TrimPrefix(line, "Name:"))
		case strings.HasPrefix(line, "PPid:"):
			ppid = strings.TrimSpace(strings.TrimPrefix(line, "PPid:"))
		}
		if ppid != "" && procName != "" {
			return ppid, procName, true
		}
	}
	return ppid, procName, ppid != "" && procName != ""
}

// isNumeric reports whether s is a non-empty string of ASCII digits.
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// SessionExists checks if a Claude session UUID has data in ~/.claude/.
// It checks ~/.claude/projects/ and ~/.claude/conversations/, returning false
// (not an error) if neither directory exists.
func SessionExists(uuid string) bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	claudeDir := filepath.Join(home, ".claude")

	// Check each candidate directory; skip ones that don't exist.
	for _, subdir := range []string{"projects", "conversations"} {
		dir := filepath.Join(claudeDir, subdir)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		}
		if sessionExistsWalk(dir, uuid) {
			return true
		}
	}

	return false
}

// sessionExistsWalk walks dir looking for any *.json file containing uuid
// as a substring. Pure Go — no grep dependency.
//
// Uses filepath.WalkDir with an early-exit sentinel on first match.
// Conservatively returns false on any I/O error (the caller's fallback path
// handles missing session data safely).
func sessionExistsWalk(dir, uuid string) bool {
	needle := []byte(uuid)
	found := false
	errFound := errors.New("match-found")

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Skip unreadable entries; don't fail the whole walk.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".json") {
			return nil
		}
		// File is small (claude sessions), read whole thing.
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if bytes.Contains(data, needle) {
			found = true
			return errFound // early exit
		}
		return nil
	})

	// Only errFound is expected; other errors fall through to "not found".
	_ = err
	return found
}
