package health

import (
	"fmt"
	"os"

	"github.com/RandomCodeSpace/ctm/internal/agent/claude"
	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

// CheckClaudeProcess checks that a claude child process is alive under the tmux pane.
func CheckClaudeProcess(tc *tmux.Client, sessionName string) CheckResult {
	panePID, err := tc.PanePID(sessionName)
	if err != nil {
		return CheckResult{
			Name:    "claude_process",
			Status:  StatusFail,
			Message: fmt.Sprintf("could not get pane PID for session %q: %v", sessionName, err),
			Fix:     "ensure the tmux session exists and has an active pane",
		}
	}

	claudePID := claude.FindClaudeChild(panePID)
	if claudePID == "" {
		return CheckResult{
			Name:    "claude_process",
			Status:  StatusFail,
			Message: fmt.Sprintf("no claude child process found under pane PID %s", panePID),
			Fix:     "start claude in the tmux session",
		}
	}

	alive, err := claude.IsClaudeAlive(claudePID)
	if err != nil {
		return CheckResult{
			Name:    "claude_process",
			Status:  StatusFail,
			Message: fmt.Sprintf("error checking claude PID %s: %v", claudePID, err),
		}
	}
	if !alive {
		return CheckResult{
			Name:    "claude_process",
			Status:  StatusFail,
			Message: fmt.Sprintf("claude process PID %s is not alive", claudePID),
			Fix:     "restart claude in the tmux session",
		}
	}

	return CheckResult{
		Name:    "claude_process",
		Status:  StatusPass,
		Message: fmt.Sprintf("claude process PID %s is alive", claudePID),
	}
}

// CheckClaudeSession verifies that a Claude session UUID exists in ~/.claude/.
func CheckClaudeSession(uuid string) CheckResult {
	if uuid == "" {
		return CheckResult{
			Name:    "claude_session",
			Status:  StatusFail,
			Message: "session UUID is empty",
			Fix:     "provide a valid Claude session UUID",
		}
	}

	if claude.SessionExists(uuid) {
		return CheckResult{
			Name:    "claude_session",
			Status:  StatusPass,
			Message: fmt.Sprintf("claude session %q exists", uuid),
		}
	}

	return CheckResult{
		Name:    "claude_session",
		Status:  StatusFail,
		Message: fmt.Sprintf("claude session %q not found in ~/.claude/", uuid),
		Fix:     "verify the session UUID or create a new claude session",
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
