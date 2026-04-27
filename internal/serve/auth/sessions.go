package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultSessionTTL bounds how long an in-memory session token remains
// valid. Caps the blast radius of a leaked token on a shared machine.
const DefaultSessionTTL = 30 * 24 * time.Hour

// sessionEntry is the internal value type for the sessions map.
type sessionEntry struct {
	username  string
	createdAt time.Time
}

// Store is a goroutine-safe in-memory map of session tokens to
// usernames. The zero value is unusable; callers must use NewStore.
// Single-user assumption: the map contains 0..N entries for a
// single username (one per device).
type Store struct {
	mu      sync.RWMutex
	entries map[string]sessionEntry

	ttl time.Duration
	now func() time.Time

	staleWindow time.Duration
	lastCheck   atomic.Int64
	userPresent atomic.Bool
	everPresent atomic.Bool // true once we've seen user.json exist
}

// NewStore constructs an empty session store with the default TTL.
func NewStore() *Store {
	return NewStoreWithTTL(DefaultSessionTTL)
}

// NewStoreWithTTL constructs an empty session store with a custom TTL.
// Exposed for tests; production code should use NewStore.
func NewStoreWithTTL(ttl time.Duration) *Store {
	return &Store{
		entries:     make(map[string]sessionEntry),
		ttl:         ttl,
		now:         time.Now,
		staleWindow: time.Second,
	}
}

// Create issues a new random 32-byte session token for username and
// returns it. Token format: base64.URL-encoded.
func (s *Store) Create(username string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("auth: rand: %w", err)
	}
	tok := base64.RawURLEncoding.EncodeToString(raw)
	s.mu.Lock()
	s.entries[tok] = sessionEntry{username: username, createdAt: s.now()}
	s.mu.Unlock()
	return tok, nil
}

// Lookup returns the username bound to tok, or ("", false) if the
// token is unknown. If user.json has been deleted since last check,
// the entire store is wiped before reporting false. Expired entries
// (older than the store's TTL) are evicted lazily and reported as
// not-found.
func (s *Store) Lookup(tok string) (string, bool) {
	if s.userFileGone() {
		s.Wipe()
		return "", false
	}
	s.mu.RLock()
	entry, ok := s.entries[tok]
	s.mu.RUnlock()
	if !ok {
		return "", false
	}
	if s.now().Sub(entry.createdAt) > s.ttl {
		s.mu.Lock()
		// Re-check under write lock to avoid racing a concurrent refresh
		// (Revoke/Wipe/Create) that may have touched this key.
		if cur, stillThere := s.entries[tok]; stillThere && s.now().Sub(cur.createdAt) > s.ttl {
			delete(s.entries, tok)
		}
		s.mu.Unlock()
		return "", false
	}
	return entry.username, true
}

// Revoke removes the given token. No-op if it doesn't exist.
func (s *Store) Revoke(tok string) {
	s.mu.Lock()
	delete(s.entries, tok)
	s.mu.Unlock()
}

// Wipe drops every session.
func (s *Store) Wipe() {
	s.mu.Lock()
	s.entries = make(map[string]sessionEntry)
	s.mu.Unlock()
}

// Seed inserts a pre-known token → username mapping. Intended only for
// test seams where the caller injects a fixed token via Options.Token.
func (s *Store) Seed(token, username string) {
	s.mu.Lock()
	s.entries[token] = sessionEntry{username: username, createdAt: s.now()}
	s.mu.Unlock()
}

// SetStaleWindowForTest lets tests force an immediate restat.
func (s *Store) SetStaleWindowForTest(d time.Duration) {
	s.staleWindow = d
	s.lastCheck.Store(0)
}

// SetClockForTest injects a fake clock for TTL tests. Not safe for
// concurrent use with live traffic; tests only.
func (s *Store) SetClockForTest(now func() time.Time) {
	s.mu.Lock()
	s.now = now
	s.mu.Unlock()
}

// EntryCountForTest returns the number of map entries. Test-only
// accessor used to assert lazy eviction.
func (s *Store) EntryCountForTest() int {
	s.mu.RLock()
	n := len(s.entries)
	s.mu.RUnlock()
	return n
}

func (s *Store) userFileGone() bool {
	now := time.Now().UnixNano()
	last := s.lastCheck.Load()
	if last != 0 && time.Duration(now-last) < s.staleWindow {
		// Only "gone" if we previously saw it present and it's now absent.
		return s.everPresent.Load() && !s.userPresent.Load()
	}
	_, err := os.Stat(UserPath())
	present := err == nil
	if present {
		s.everPresent.Store(true)
	}
	s.userPresent.Store(present)
	s.lastCheck.Store(now)
	// Only "gone" if we previously saw it present and it's now absent.
	return s.everPresent.Load() && !present
}
