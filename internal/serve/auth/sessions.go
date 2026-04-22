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

// Store is a goroutine-safe in-memory map of session tokens to
// usernames. The zero value is unusable; callers must use NewStore.
// Single-user assumption: the map contains 0..N entries for a
// single username (one per device).
type Store struct {
	mu      sync.RWMutex
	entries map[string]string

	staleWindow  time.Duration
	lastCheck    atomic.Int64
	userPresent  atomic.Bool
	everPresent  atomic.Bool // true once we've seen user.json exist
}

// NewStore constructs an empty session store.
func NewStore() *Store {
	return &Store{
		entries:     make(map[string]string),
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
	s.entries[tok] = username
	s.mu.Unlock()
	return tok, nil
}

// Lookup returns the username bound to tok, or ("", false) if the
// token is unknown. If user.json has been deleted since last check,
// the entire store is wiped before reporting false.
func (s *Store) Lookup(tok string) (string, bool) {
	if s.userFileGone() {
		s.Wipe()
		return "", false
	}
	s.mu.RLock()
	user, ok := s.entries[tok]
	s.mu.RUnlock()
	return user, ok
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
	s.entries = make(map[string]string)
	s.mu.Unlock()
}

// SetStaleWindowForTest lets tests force an immediate restat.
func (s *Store) SetStaleWindowForTest(d time.Duration) {
	s.staleWindow = d
	s.lastCheck.Store(0)
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
