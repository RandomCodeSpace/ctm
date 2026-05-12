// Package agent defines the multi-agent abstraction. Each supported
// agent CLI (claude, codex, future opencode) implements Agent and is
// registered via Register() in its package's init() — see
// internal/agent/claude and internal/agent/codex.
//
// cmd/* consumes agents via For(sess.Agent). The Agent interface is
// the only seam between session-level code (cmd/, internal/session/)
// and per-agent specifics (CLI flags, hook event names, file layouts).
package agent

// Agent is the per-CLI behavior contract. Methods return values
// rather than mutating the agent — Agent values are expected to be
// safe to use from multiple goroutines without external locking.
type Agent interface {
	// Name returns the canonical short identifier (e.g. "claude",
	// "codex"). Used as the on-disk Session.Agent value and as the
	// registry key.
	Name() string

	// Binary returns the executable name searched in PATH (e.g.
	// "claude", "codex"). Implementations may honor CTM_<UPPER-NAME>_BIN
	// env var to support fake-agent fixture binaries in tests.
	Binary() string

	// DefaultSessionName returns the tmux session name when the user
	// runs `ctm yolo --agent <name>` without a positional name arg.
	DefaultSessionName() string

	// ProcessName returns the comm/name string used to identify a
	// child process under /proc/<pid>/status for pane-PID discovery.
	ProcessName() string

	// BuildCommand returns the shell command that tmux's `new-session`
	// runs as the pane process. The result is embedded verbatim in
	// `sh -c <result>`, so it must be shell-safe.
	BuildCommand(SpawnSpec) string

	// YOLOFlag returns the agent-CLI flag(s) appended to the spawn
	// command when SpawnSpec.Mode == "yolo". Empty slice when the
	// agent has no bypass flag.
	YOLOFlag() []string
}

// SpawnSpec is the per-spawn input passed to Agent.BuildCommand.
type SpawnSpec struct {
	// UUID is ctm's own session UUID (Session.UUID). For claude this
	// also becomes the agent's session-id (`claude --session-id`);
	// for codex it is unused at spawn time and only carried for
	// observability.
	UUID string

	// AgentSessionID is the per-agent backend session/thread ID
	// (Session.AgentSessionID). For claude == UUID. For codex it is
	// the thread UUID discovered post-spawn; empty on first run.
	AgentSessionID string

	// Mode is "safe" or "yolo". Drives YOLOFlag insertion.
	Mode string

	// Resume requests resume semantics. False on first spawn; true
	// on reattach where conversation continuity is desired.
	Resume bool

	// OverlayPath is the resolved overlay file path or empty when
	// the user has not initialized an overlay. The agent's
	// BuildCommand is responsible for any TOCTOU-safe shell guard.
	OverlayPath string

	// EnvExports is a pre-built `export K='V' …` shell prelude
	// produced from <agent>-env.json. Empty when no env file exists.
	EnvExports string
}
