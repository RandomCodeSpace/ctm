// Package procscan exposes the small set of /proc helpers ctm uses to
// detect whether the per-pane agent process is alive. It deliberately
// has no agent-specific knowledge — callers supply the comm name they
// expect (e.g. "codex", "claude" historically) and procscan walks
// /proc/*/status with no further coupling.
package procscan

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
)

// IsAlive reports whether the PID names a non-zombie process. Returns
// (false, nil) when the PID directory is absent — a "definitely dead"
// signal that callers handle the same as a zombie. Returns an error
// only for malformed PIDs (empty or non-numeric).
func IsAlive(pid string) (bool, error) {
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

// FindChild walks /proc/*/status looking for the first PID whose
// PPid matches parentPID and whose Name matches procName. Returns an
// empty string if no match is found.
//
// Pure Go — no pgrep dependency, no shell invocations.
func FindChild(parentPID, procName string) string {
	if parentPID == "" || procName == "" {
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
		ppid, comm, ok := readStatus("/proc/" + name + "/status")
		if !ok {
			continue
		}
		if ppid == parentPID && comm == procName {
			return name
		}
	}
	return ""
}

func readStatus(path string) (ppid, comm string, ok bool) {
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
			comm = strings.TrimSpace(strings.TrimPrefix(line, "Name:"))
		case strings.HasPrefix(line, "PPid:"):
			ppid = strings.TrimSpace(strings.TrimPrefix(line, "PPid:"))
		}
		if ppid != "" && comm != "" {
			return ppid, comm, true
		}
	}
	return ppid, comm, ppid != "" && comm != ""
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
