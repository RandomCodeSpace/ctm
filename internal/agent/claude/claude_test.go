package claude_test

import (
	"strings"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/agent"
	_ "github.com/RandomCodeSpace/ctm/internal/agent/claude" // register via init
)

func TestClaude_IsRegistered(t *testing.T) {
	a, ok := agent.For("claude")
	if !ok {
		t.Fatal("claude not registered after blank import")
	}
	if a.Name() != "claude" {
		t.Fatalf("Name = %q, want claude", a.Name())
	}
	if a.ProcessName() != "claude" {
		t.Fatalf("ProcessName = %q, want claude", a.ProcessName())
	}
	if a.DefaultSessionName() != "claude" {
		t.Fatalf("DefaultSessionName = %q, want claude", a.DefaultSessionName())
	}
}

func TestClaude_Binary_DefaultsLiteral(t *testing.T) {
	t.Setenv("CTM_CLAUDE_BIN", "")
	a, _ := agent.For("claude")
	if got := a.Binary(); got != "claude" {
		t.Fatalf("Binary = %q, want claude", got)
	}
}

func TestClaude_Binary_HonorsEnvOverride(t *testing.T) {
	t.Setenv("CTM_CLAUDE_BIN", "/fake/path/to/claude")
	a, _ := agent.For("claude")
	if got := a.Binary(); got != "/fake/path/to/claude" {
		t.Fatalf("Binary = %q, want /fake/path/to/claude", got)
	}
}

func TestClaude_BuildCommand_FreshSafe(t *testing.T) {
	a, _ := agent.For("claude")
	got := a.BuildCommand(agent.SpawnSpec{UUID: "u-1", Mode: "safe"})
	want := "claude --session-id u-1"
	if got != want {
		t.Fatalf("BuildCommand = %q, want %q", got, want)
	}
}

func TestClaude_BuildCommand_FreshYolo(t *testing.T) {
	a, _ := agent.For("claude")
	got := a.BuildCommand(agent.SpawnSpec{UUID: "u-1", Mode: "yolo"})
	want := "claude --session-id u-1 --dangerously-skip-permissions"
	if got != want {
		t.Fatalf("BuildCommand = %q, want %q", got, want)
	}
}

func TestClaude_BuildCommand_ResumeFallbackChain(t *testing.T) {
	a, _ := agent.For("claude")
	got := a.BuildCommand(agent.SpawnSpec{UUID: "u-1", Mode: "safe", Resume: true})
	if !strings.Contains(got, "claude --resume u-1") {
		t.Fatalf("expected --resume branch, got: %q", got)
	}
	if !strings.Contains(got, "|| claude --session-id u-1") {
		t.Fatalf("expected fallback chain, got: %q", got)
	}
}

func TestClaude_YOLOFlag(t *testing.T) {
	a, _ := agent.For("claude")
	flags := a.YOLOFlag()
	if len(flags) != 1 || flags[0] != "--dangerously-skip-permissions" {
		t.Fatalf("YOLOFlag = %v, want [--dangerously-skip-permissions]", flags)
	}
}
