package codex

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

// IsCodexAlive checks if a PID exists and is not a zombie.
func IsCodexAlive(pid string) (bool, error) {
	if pid == "" {
		return false, errors.New("pid must not be empty")
	}

	var pidInt int
	if _, err := fmt.Sscanf(pid, "%d", &pidInt); err != nil || pidInt <= 0 {
		return false, fmt.Errorf("invalid pid: %q", pid)
	}

	statusPath := fmt.Sprintf("/proc/%s/status", pid)
	f, err := os.Open(statusPath)
	if err != nil {
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

// FindCodexChild finds a codex process among children of the given PID by
// walking /proc/*/status. Pure Go — no pgrep dependency.
func FindCodexChild(panePID string) string {
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
		name := e.Name()
		if !isNumeric(name) {
			continue
		}

		ppid, procName, ok := readProcStatus("/proc/" + name + "/status")
		if !ok {
			continue
		}
		if ppid == panePID && procName == "codex" {
			return name
		}
	}

	return ""
}

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

