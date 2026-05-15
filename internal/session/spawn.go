package session

// Reusable yolo-spawn shared by the CLI yolo path and any future
// programmatic spawner. Attach is intentionally NOT part of this
// function — the caller wraps this + tmux attach.

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/agent"
)

// TmuxSpawner is the narrow slice of *tmux.Client Yolo needs.
type TmuxSpawner interface {
	NewSession(name, workdir, shellCmd string) error
	SendKeys(target, keys string) error
	KillSession(name string) error
}

// AgentSessionStamper persists a discovered agent-side session/thread
// identifier onto the named session row. *Store satisfies this via
// UpdateAgentSessionID. The Saver/Stamper split keeps tests honest —
// fakes can satisfy Saver alone and skip the discovery write path.
type AgentSessionStamper interface {
	UpdateAgentSessionID(name, id string) error
}

// Saver is the narrow slice of *Store Yolo needs.
type Saver interface {
	Save(sess *Session) error
}

// SpawnOpts bundles the tmux client and store so Yolo can be driven
// from either CLI or daemon without depending on config globals.
//
// Agent selects the registered agent.Agent driving the pane. Empty is
// normalized to DefaultAgent (codex) by the call below.
//
// EnvExports is a pre-built shell-export prelude produced by the
// caller (e.g. config.CodexEnvExports). Empty when no env file is
// present.
//
// OverlayPath is currently unused by the codex agent; the field is
// retained as part of agent.SpawnSpec so a future agent that wants a
// settings overlay can wire it through.
type SpawnOpts struct {
	Name        string
	Agent       string
	Workdir     string
	Tmux        TmuxSpawner
	Store       Saver
	OverlayPath string
	EnvExports  string

	// OnDiscoveryComplete fires when the background DiscoverSessionID
	// goroutine returns (success, timeout, or no stamper). Optional —
	// production callers leave it nil. Tests pass a chan-close func to
	// synchronize on completion before asserting on AgentSessionID.
	OnDiscoveryComplete func()
}

// Yolo creates a detached tmux session, launches the configured agent
// in yolo mode, and persists the session state. Returns the populated
// Session.
//
// Preconditions:
//   - Workdir must be absolute
//   - Workdir must exist and be a directory
//   - Agent (or DefaultAgent on empty) must be registered
//
// Postconditions on success: tmux session exists detached, the agent
// is launched inside it, Store.Save has been called with mode="yolo".
// On error before Save: NO session state is persisted.
func Yolo(opts SpawnOpts) (Session, error) {
	if !filepath.IsAbs(opts.Workdir) {
		return Session{}, fmt.Errorf("workdir must be absolute: %q", opts.Workdir)
	}
	info, err := os.Stat(opts.Workdir)
	if err != nil {
		return Session{}, fmt.Errorf("workdir stat: %w", err)
	}
	if !info.IsDir() {
		return Session{}, fmt.Errorf("workdir is not a directory: %q", opts.Workdir)
	}

	agentName := opts.Agent
	if agentName == "" || agentName == "claude" {
		agentName = DefaultAgent
	}
	a, ok := agent.For(agentName)
	if !ok {
		return Session{}, fmt.Errorf("unknown agent %q (registered: %v)", agentName, agent.Registered())
	}

	uid := newUUIDv4()
	shellCmd := a.BuildCommand(agent.SpawnSpec{
		UUID:        uid,
		Mode:        "yolo",
		Resume:      false,
		OverlayPath: opts.OverlayPath,
		EnvExports:  opts.EnvExports,
	})

	spawnStart := time.Now()
	if err := opts.Tmux.NewSession(opts.Name, opts.Workdir, shellCmd); err != nil {
		return Session{}, fmt.Errorf("tmux new-session: %w", err)
	}

	sess := Session{
		Name:    opts.Name,
		UUID:    uid,
		Mode:    "yolo",
		Workdir: opts.Workdir,
		Agent:   agentName,
	}
	if err := opts.Store.Save(&sess); err != nil {
		// Best-effort cleanup of the orphan tmux session we just created.
		// Ignore the kill error — Save's err is the meaningful one.
		_ = opts.Tmux.KillSession(opts.Name)
		return Session{}, fmt.Errorf("session save: %w", err)
	}

	// Fire-and-forget: discover the agent's backend session/thread ID
	// in the background so the user's interactive attach isn't blocked.
	// On success we stamp it onto the store row so future reattach
	// uses `codex resume <id>` instead of `--last`. On timeout / no
	// stamper / no discovery, the row stays as-is.
	stamper, hasStamper := opts.Store.(AgentSessionStamper)
	if hasStamper {
		go discoverAndStamp(a, opts.Name, spawnStart, stamper, opts.OnDiscoveryComplete)
	} else if opts.OnDiscoveryComplete != nil {
		// No stamper means no discovery happened — fire the callback
		// immediately so tests waiting on it don't deadlock.
		opts.OnDiscoveryComplete()
	}
	return sess, nil
}

// discoverAndStamp runs the agent's DiscoverSessionID and persists the
// result via stamper. Errors are logged at debug level — the discovery
// is opportunistic, never load-bearing. onComplete (if non-nil) is
// invoked after the stamp attempt regardless of outcome.
func discoverAndStamp(a agent.Agent, name string, spawnStart time.Time, stamper AgentSessionStamper, onComplete func()) {
	defer func() {
		if onComplete != nil {
			onComplete()
		}
	}()
	id, ok := a.DiscoverSessionID(spawnStart)
	if !ok {
		slog.Debug("agent session id discovery timed out",
			"session", name, "agent", a.Name())
		return
	}
	if err := stamper.UpdateAgentSessionID(name, id); err != nil {
		slog.Debug("could not stamp agent session id",
			"session", name, "agent", a.Name(), "id", id, "err", err)
		return
	}
	slog.Debug("stamped agent session id",
		"session", name, "agent", a.Name(), "id", id)
}
