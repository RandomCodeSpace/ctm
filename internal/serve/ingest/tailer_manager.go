package ingest

import (
	"context"
	"sync"

	"github.com/RandomCodeSpace/ctm/internal/serve/events"
)

// TailerManager owns the lifecycle of one Tailer goroutine per active
// session. Hooks (`session_new` → Start, `session_killed` → Stop) and
// the startup reconciliation sweep both call into this manager.
type TailerManager struct {
	logDir string
	hub    *events.Hub

	mu      sync.Mutex
	tailers map[string]*handle // keyed on human session name
}

type handle struct {
	uuid   string
	cancel context.CancelFunc
	done   chan struct{}
}

// NewTailerManager constructs a manager rooted at logDir. logDir is
// typically `~/.config/ctm/logs/`; tests pass a t.TempDir().
func NewTailerManager(logDir string, hub *events.Hub) *TailerManager {
	return &TailerManager{
		logDir:  logDir,
		hub:     hub,
		tailers: make(map[string]*handle),
	}
}

// Start spawns a tailer for (name, uuid) if one isn't already running
// for that name. Re-calling with the same name and the same uuid is a
// no-op; calling with a different uuid implicitly stops the prior
// tailer first (rare — happens on uuid drift after recreation).
func (m *TailerManager) Start(ctx context.Context, name, uuid string) {
	if name == "" || uuid == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	if h, ok := m.tailers[name]; ok {
		if h.uuid == uuid {
			return
		}
		m.stopLocked(name, h)
	}

	tCtx, cancel := context.WithCancel(ctx)
	h := &handle{uuid: uuid, cancel: cancel, done: make(chan struct{})}
	m.tailers[name] = h

	t := NewTailer(name, uuid, m.logDir, m.hub)
	go func() {
		defer close(h.done)
		_ = t.Run(tCtx)
	}()
}

// Stop terminates the tailer for the named session, if any. Blocks
// until the goroutine exits (ensures no late publishes after shutdown).
func (m *TailerManager) Stop(name string) {
	m.mu.Lock()
	h, ok := m.tailers[name]
	if !ok {
		m.mu.Unlock()
		return
	}
	m.stopLocked(name, h)
	m.mu.Unlock()
}

// StopAll terminates every running tailer. Used during graceful
// shutdown of the serve daemon.
func (m *TailerManager) StopAll() {
	m.mu.Lock()
	names := make([]string, 0, len(m.tailers))
	for name := range m.tailers {
		names = append(names, name)
	}
	for _, name := range names {
		if h, ok := m.tailers[name]; ok {
			m.stopLocked(name, h)
		}
	}
	m.mu.Unlock()
}

// Active reports the names of currently-running tailers (test helper /
// debug aid).
func (m *TailerManager) Active() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, 0, len(m.tailers))
	for name := range m.tailers {
		names = append(names, name)
	}
	return names
}

func (m *TailerManager) stopLocked(name string, h *handle) {
	delete(m.tailers, name)
	h.cancel()
	<-h.done
}
