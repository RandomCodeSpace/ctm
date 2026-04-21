package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/git"
)

// checkpointsCacheTTL is the per-session window during which repeated
// GETs return the previously rendered list without re-running git.
// The 5 s figure mirrors the spec's "/checkpoints (5 s server cache)".
const checkpointsCacheTTL = 5 * time.Second

// checkpointsLister is the seam tests inject; production code passes
// git.List. Kept package-private — callers wire through Checkpoints().
var checkpointsLister = git.List

type checkpointsCacheEntry struct {
	at    time.Time
	value []git.Checkpoint
	limit int
}

// CheckpointsCache wraps the per-session 5 s TTL cache used by the
// /checkpoints handler so other callers — notably the revert
// SHA-allowlist check — can reuse the same cached list rather than
// each spinning up its own `git log` subprocess.
//
// The zero value is ready to use; NewCheckpointsCache exists for
// symmetry with the rest of the api package.
type CheckpointsCache struct {
	mu      sync.RWMutex
	entries map[string]checkpointsCacheEntry
	listFn  func(workdir string, limit int) ([]git.Checkpoint, error)
}

// NewCheckpointsCache constructs a fresh cache. Lister selection is
// deferred to call time so tests can swap the package-level
// `checkpointsLister` after construction and still see the change.
func NewCheckpointsCache() *CheckpointsCache {
	return &CheckpointsCache{}
}

// Get returns the cached checkpoint list for (name, limit) when fresh,
// otherwise re-runs the underlying lister against workdir, caches, and
// returns. Errors propagate without poisoning the cache.
func (c *CheckpointsCache) Get(workdir, name string, limit int) ([]git.Checkpoint, error) {
	if cached, ok := c.lookup(name, limit); ok {
		return cached, nil
	}
	listFn := c.listFn
	if listFn == nil {
		listFn = checkpointsLister
	}
	fresh, err := listFn(workdir, limit)
	if err != nil {
		return nil, err
	}
	c.store(name, limit, fresh)
	return fresh, nil
}

// IsCheckpoint reports whether sha is one of the *full* commit SHAs
// returned for (workdir, name) by Get(workdir, name, 200). Comparison
// is exact-match only — abbreviated SHAs are intentionally rejected
// because prefix matching would expand the allowlist's blast radius.
func (c *CheckpointsCache) IsCheckpoint(workdir, name, sha string) bool {
	if sha == "" {
		return false
	}
	cps, err := c.Get(workdir, name, 200)
	if err != nil {
		return false
	}
	for _, cp := range cps {
		if cp.SHA == sha {
			return true
		}
	}
	return false
}

func (c *CheckpointsCache) lookup(name string, limit int) ([]git.Checkpoint, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.entries[name]
	if !ok || e.limit != limit || time.Since(e.at) > checkpointsCacheTTL {
		return nil, false
	}
	return e.value, true
}

func (c *CheckpointsCache) store(name string, limit int, value []git.Checkpoint) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]checkpointsCacheEntry)
	}
	c.entries[name] = checkpointsCacheEntry{
		at:    time.Now(),
		value: value,
		limit: limit,
	}
}

// Checkpoints returns the GET handler for
// /api/sessions/{name}/checkpoints. resolveWorkdir lets the handler
// stay decoupled from the sessions package: it returns the workdir
// for `name` and false when the session is unknown. cache is shared
// with the revert handler's SHA-allowlist check (see server.go).
func Checkpoints(resolveWorkdir func(name string) (string, bool), cache *CheckpointsCache) http.HandlerFunc {
	if cache == nil {
		cache = NewCheckpointsCache()
	}
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		name := r.PathValue("name")
		if name == "" {
			http.NotFound(w, r)
			return
		}

		workdir, ok := resolveWorkdir(name)
		if !ok {
			http.NotFound(w, r)
			return
		}

		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			if n, err := strconv.Atoi(raw); err == nil && n > 0 {
				limit = n
			}
		}

		list, err := cache.Get(workdir, name, limit)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "git_failed"})
			return
		}
		if list == nil {
			list = []git.Checkpoint{}
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(list)
	}
}
