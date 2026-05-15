package health

import (
	"fmt"
	"os"

	"github.com/RandomCodeSpace/ctm/internal/procscan"
	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

// CheckAgentProcess verifies that a child process named procName is
// alive under the tmux pane for sessionName. procName is the value the
// agent reports via Agent.ProcessName() — "codex" for the codex agent.
// The check is agent-agnostic; this package has no dependency on any
// specific agent implementation.
func CheckAgentProcess(tc *tmux.Client, sessionName, procName string) CheckResult {
	checkName := procName + "_process"
	panePID, err := tc.PanePID(sessionName)
	if err != nil {
		return CheckResult{
			Name:    checkName,
			Status:  StatusFail,
			Message: fmt.Sprintf("could not get pane PID for session %q: %v", sessionName, err),
			Fix:     "ensure the tmux session exists and has an active pane",
		}
	}

	pid := procscan.FindChild(panePID, procName)
	if pid == "" {
		return CheckResult{
			Name:    checkName,
			Status:  StatusFail,
			Message: fmt.Sprintf("no %s child process found under pane PID %s", procName, panePID),
			Fix:     fmt.Sprintf("start %s in the tmux session", procName),
		}
	}

	alive, err := procscan.IsAlive(pid)
	if err != nil {
		return CheckResult{
			Name:    checkName,
			Status:  StatusFail,
			Message: fmt.Sprintf("error checking %s PID %s: %v", procName, pid, err),
		}
	}
	if !alive {
		return CheckResult{
			Name:    checkName,
			Status:  StatusFail,
			Message: fmt.Sprintf("%s process PID %s is not alive", procName, pid),
			Fix:     fmt.Sprintf("restart %s in the tmux session", procName),
		}
	}

	return CheckResult{
		Name:    checkName,
		Status:  StatusPass,
		Message: fmt.Sprintf("%s process PID %s is alive", procName, pid),
	}
}

// CheckWorkdir verifies that the given directory exists and is a directory.
func CheckWorkdir(workdir string) CheckResult {
	if workdir == "" {
		return CheckResult{
			Name:    "workdir",
			Status:  StatusFail,
			Message: "working directory not set (migrated session)",
			Fix:     "ctm forget <name> && ctm new <name> /path/to/project",
		}
	}

	info, err := os.Stat(workdir)
	if err != nil {
		if os.IsNotExist(err) {
			return CheckResult{
				Name:    "workdir",
				Status:  StatusFail,
				Message: fmt.Sprintf("workdir %q does not exist", workdir),
				Fix:     fmt.Sprintf("create the directory: mkdir -p %s", workdir),
			}
		}
		return CheckResult{
			Name:    "workdir",
			Status:  StatusFail,
			Message: fmt.Sprintf("error checking workdir %q: %v", workdir, err),
		}
	}

	if !info.IsDir() {
		return CheckResult{
			Name:    "workdir",
			Status:  StatusFail,
			Message: fmt.Sprintf("workdir %q exists but is not a directory", workdir),
			Fix:     fmt.Sprintf("remove the file and create a directory at %s", workdir),
		}
	}

	return CheckResult{
		Name:    "workdir",
		Status:  StatusPass,
		Message: fmt.Sprintf("workdir %q exists and is a directory", workdir),
	}
}
