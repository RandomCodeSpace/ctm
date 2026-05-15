package agent

import (
	"fmt"
	"sort"
	"sync"
)

var (
	mu       sync.RWMutex
	registry = map[string]Agent{}
)

// Register adds a to the registry under a.Name(). Panics on duplicate
// or nil. Intended to be called from package init() in each agent's
// package; not safe to call after process startup once cmd/* has
// begun reading sessions.
func Register(a Agent) {
	if a == nil {
		panic("agent.Register: nil Agent")
	}
	mu.Lock()
	defer mu.Unlock()
	name := a.Name()
	if name == "" {
		panic("agent.Register: empty Name()")
	}
	if _, exists := registry[name]; exists {
		panic(fmt.Sprintf("agent.Register: duplicate %q", name))
	}
	registry[name] = a
}

// For looks up the agent named s. Returns ok=false if absent.
func For(s string) (Agent, bool) {
	mu.RLock()
	defer mu.RUnlock()
	a, ok := registry[s]
	return a, ok
}

// MustFor is For with a panic on miss. Convenience for code that has
// already validated the name (e.g., during Session.Save).
func MustFor(s string) Agent {
	a, ok := For(s)
	if !ok {
		panic(fmt.Sprintf("agent.MustFor: unknown %q", s))
	}
	return a
}

// Registered returns the sorted list of registered agent names.
func Registered() []string {
	mu.RLock()
	defer mu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Reset clears the registry. Test-only — call from TestMain or in a
// test that wires up its own stubs.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	registry = map[string]Agent{}
}
