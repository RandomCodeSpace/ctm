package claude

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
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

// FindClaudeChild finds a claude process among children of the given PID.
func FindClaudeChild(panePID string) string {
	if panePID == "" {
		return ""
	}

	// Use -l (not -a) for POSIX compatibility — -a is Linux-only.
	// -l outputs "PID name" where name is the process basename.
	out, err := exec.Command("pgrep", "-P", panePID, "-l").Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(out), "\n")
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		// fields[0] is PID; fields[1] is process name (basename only with -l).
		if fields[1] == "claude" {
			return fields[0]
		}
	}

	return ""
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

func sessionExistsWalk(dir, uuid string) bool {
	// grep exit codes: 0=match, 1=no match, 2=error.
	// We conservatively report "not found" in both non-zero cases since
	// the fallback (--session-id for fresh session) is safe. Callers who
	// need to distinguish should use a different API.
	out, err := exec.Command("grep", "-r", uuid, dir, "--include=*.json", "-l").Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) != ""
}
