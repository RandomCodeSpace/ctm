package health

import (
	"fmt"

	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

// CheckTmuxSession verifies that a tmux session with the given name exists.
func CheckTmuxSession(client *tmux.Client, name string) CheckResult {
	if client.HasSession(name) {
		return CheckResult{
			Name:    "tmux_session",
			Status:  StatusPass,
			Message: fmt.Sprintf("tmux session %q exists", name),
		}
	}
	return CheckResult{
		Name:    "tmux_session",
		Status:  StatusFail,
		Message: fmt.Sprintf("tmux session %q not found", name),
		Fix:     fmt.Sprintf("run: tmux new-session -d -s %s", name),
	}
}
