package claude

import (
	"testing"
)

func TestIsClaudePID_InvalidPID(t *testing.T) {
	alive, err := IsClaudeAlive("")
	if err == nil {
		t.Error("expected error for empty PID, got nil")
	}
	if alive {
		t.Error("expected not alive for empty PID")
	}
}

func TestIsClaudePID_NonExistent(t *testing.T) {
	alive, err := IsClaudeAlive("9999999")
	if err != nil {
		t.Errorf("expected no error for non-existent PID, got: %v", err)
	}
	if alive {
		t.Error("expected not alive for non-existent PID")
	}
}

func TestFindClaudeChild_NoPID(t *testing.T) {
	pid := FindClaudeChild("")
	if pid != "" {
		t.Errorf("expected empty string for empty panePID, got: %s", pid)
	}
}
