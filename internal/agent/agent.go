// Package agent defines the multi-agent abstraction. Each supported
// agent CLI implements Agent and is registered via Register() in its
// package's init() — see internal/agent/codex for the current default
// (and only built-in) implementation.
//
// cmd/* consumes agents via For(sess.Agent). The Agent interface is
// the only seam between session-level code (cmd/, internal/session/)
// and per-agent specifics (CLI flags, hook event names, file layouts).
// Future agents (e.g. opencode) plug in without touching the call
// sites.
package agent

import "time"

// Agent is the per-CLI behavior contract. Methods return values
// rather than mutating the agent — Agent values are expected to be
// safe to use from multiple goroutines without external locking.
type Agent interface {
	// Name returns the canonical short identifier (e.g. "codex").
	// Used as the on-disk Session.Agent value and as the registry
	// key.
	Name() string

	// Binary returns the executable name searched in PATH (e.g.
	// "codex"). Implementations may honor CTM_<UPPER-NAME>_BIN env
	// var to support fake-agent fixture binaries in tests.
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

	// DiscoverSessionID polls the agent's on-disk state for the
	// thread/session identifier created by a fresh spawn that started
	// at spawnStart. Implementations pick their own polling cadence and
	// budget. Returns ("", false) on timeout, missing state directory,
	// or any agent whose AgentSessionID is not separable from
	// SpawnSpec.UUID (e.g. claude historically used UUID directly).
	//
	// The returned ID is stored in Session.AgentSessionID so future
	// resume can target the specific thread (codex resume <id>) instead
	// of falling through to picker / --last.
	DiscoverSessionID(spawnStart time.Time) (string, bool)
}

// SpawnSpec is the per-spawn input passed to Agent.BuildCommand.
type SpawnSpec struct {
	// UUID is ctm's own session UUID (Session.UUID). For codex it is
	// unused at spawn time and only carried for observability — the
	// agent-side session/thread ID lives in AgentSessionID below.
	UUID string

	// AgentSessionID is the per-agent backend session/thread ID
	// (Session.AgentSessionID). For codex it is the thread UUID
	// discovered post-spawn; empty on first run.
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
