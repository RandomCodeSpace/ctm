// Package hermes provides the Agent implementation for the Hermes Agent CLI.
//
// Registration happens in init() — any binary that links this package will
// have "hermes" available in the agent registry.
package hermes

import (
	"os"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/agent"
)

func init() {
	agent.Register(New())
}

type hermesAgent struct{}

// New returns the hermes Agent. Exposed (not just via init) so test code
// that needs a fresh registry can re-register after agent.Reset().
func New() agent.Agent { return hermesAgent{} }

func (hermesAgent) Name() string               { return "hermes" }
func (hermesAgent) DefaultSessionName() string { return "hermes" }
func (hermesAgent) ProcessName() string        { return "hermes" }

// Binary honors CTM_HERMES_BIN for fake-binary fixture overrides in
// integration tests. Production deployments leave it unset → "hermes" is
// resolved through PATH at exec time.
func (hermesAgent) Binary() string {
	if b := os.Getenv("CTM_HERMES_BIN"); b != "" {
		return b
	}
	return "hermes"
}

// BuildCommand delegates to the package-level BuildCommand (command.go).
func (hermesAgent) BuildCommand(s agent.SpawnSpec) string {
	return BuildCommand(s.AgentSessionID, s.Mode, s.Resume, s.EnvExports)
}

// YOLOFlag is hermes' single bypass-all-prompts flag.
func (hermesAgent) YOLOFlag() []string {
	return []string{"--yolo"}
}

// DiscoverSessionID polls hermes' on-disk state for the session created by
// a fresh spawn at spawnStart. See discover.go for the polling contract.
func (hermesAgent) DiscoverSessionID(spawnStart time.Time) (string, bool) {
	return DiscoverSessionID(spawnStart)
}
