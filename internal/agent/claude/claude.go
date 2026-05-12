// Package claude provides the Agent implementation for Anthropic's
// `claude` CLI. Spawning, resume command construction, and process
// discovery were the original ctm internals — this file wraps those
// functions in the multi-agent Agent interface so cmd/* can dispatch
// uniformly via agent.For(sess.Agent).
//
// Registration happens in init() — any binary that links this package
// will have "claude" available in the agent registry.
package claude

import (
	"os"

	"github.com/RandomCodeSpace/ctm/internal/agent"
)

func init() {
	agent.Register(New())
}

// claudeAgent is the zero-state Agent value for claude. All behavior
// is in methods; no per-instance state is required.
type claudeAgent struct{}

// New returns the claude Agent. Exposed (not just via init) so test
// code that needs a fresh registry can re-register after agent.Reset().
func New() agent.Agent { return claudeAgent{} }

func (claudeAgent) Name() string               { return "claude" }
func (claudeAgent) DefaultSessionName() string { return "claude" }
func (claudeAgent) ProcessName() string        { return "claude" }

// Binary honors CTM_CLAUDE_BIN for fake-binary fixture overrides in
// integration tests. Production deployments leave it unset → "claude"
// is resolved through PATH at exec time.
func (claudeAgent) Binary() string {
	if b := os.Getenv("CTM_CLAUDE_BIN"); b != "" {
		return b
	}
	return "claude"
}

// BuildCommand delegates to the original package-level BuildCommand
// (command.go). The Agent-flavored adapter resolves the overlay path
// (TOCTOU-safe gate) and forwards the rest.
func (claudeAgent) BuildCommand(s agent.SpawnSpec) string {
	overlay := OverlayPathIfExists(s.OverlayPath)
	return BuildCommand(s.UUID, s.Mode, s.Resume, overlay, s.EnvExports)
}

// YOLOFlag is the bypass-permissions flag claude accepts. Returned as
// a slice so cmd/yolo.go can consume it without string parsing.
// BuildCommand currently inlines this flag via Mode == "yolo", so the
// slice form is primarily for future cmd/* code that builds argv
// directly rather than via the shell-command string.
func (claudeAgent) YOLOFlag() []string {
	return []string{"--dangerously-skip-permissions"}
}
