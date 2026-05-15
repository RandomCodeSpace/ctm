// Package codex provides the Agent implementation for OpenAI's `codex` CLI.
// Registration happens in init() — any binary that links this package will
// have "codex" available in the agent registry.
package codex

import (
	"os"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/agent"
)

func init() {
	agent.Register(New())
}

// codexAgent is the zero-state Agent value for codex. All behavior is in
// methods; no per-instance state is required.
type codexAgent struct{}

// New returns the codex Agent. Exposed (not just via init) so test code
// that needs a fresh registry can re-register after agent.Reset().
func New() agent.Agent { return codexAgent{} }

func (codexAgent) Name() string               { return "codex" }
func (codexAgent) DefaultSessionName() string { return "codex" }
func (codexAgent) ProcessName() string        { return "codex" }

// Binary honors CTM_CODEX_BIN for fake-binary fixture overrides in
// integration tests. Production deployments leave it unset → "codex" is
// resolved through PATH at exec time.
func (codexAgent) Binary() string {
	if b := os.Getenv("CTM_CODEX_BIN"); b != "" {
		return b
	}
	return "codex"
}

// BuildCommand delegates to the package-level BuildCommand (command.go).
// SpawnSpec.OverlayPath is unused — codex reads ~/.codex/config.toml
// natively; ctm does not maintain a parallel overlay layer for it.
func (codexAgent) BuildCommand(s agent.SpawnSpec) string {
	return BuildCommand(s.AgentSessionID, s.Mode, s.Resume, s.EnvExports)
}

// YOLOFlag is the sandbox-bypass flag codex accepts. Returned as a slice
// so cmd/* can consume it without string parsing.
func (codexAgent) YOLOFlag() []string {
	return []string{"--sandbox", "danger-full-access"}
}

// DiscoverSessionID polls ~/.codex/sessions/ for the rollout file
// created by a fresh spawn at spawnStart. See discover.go for the
// polling contract.
func (codexAgent) DiscoverSessionID(spawnStart time.Time) (string, bool) {
	return DiscoverSessionID(spawnStart)
}
