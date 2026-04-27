package auth_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/auth"
)

func TestStore_CreateLookup(t *testing.T) {
	withTempHome(t)
	s := auth.NewStore()
	token, err := s.Create("alice")
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("empty token")
	}
	user, ok := s.Lookup(token)
	if !ok || user != "alice" {
		t.Fatalf("Lookup = (%q, %v), want (\"alice\", true)", user, ok)
	}
}

func TestStore_LookupUnknown(t *testing.T) {
	withTempHome(t)
	s := auth.NewStore()
	if _, ok := s.Lookup("nope"); ok {
		t.Fatal("Lookup of unknown token returned ok=true")
	}
}

func TestStore_Revoke(t *testing.T) {
	withTempHome(t)
	s := auth.NewStore()
	tok, _ := s.Create("alice")
	s.Revoke(tok)
	if _, ok := s.Lookup(tok); ok {
		t.Fatal("Lookup after Revoke returned ok=true")
	}
}

func TestStore_Wipe(t *testing.T) {
	withTempHome(t)
	s := auth.NewStore()
	t1, _ := s.Create("alice")
	t2, _ := s.Create("alice")
	s.Wipe()
	if _, ok := s.Lookup(t1); ok {
		t.Fatal("token t1 still present after Wipe")
	}
	if _, ok := s.Lookup(t2); ok {
		t.Fatal("token t2 still present after Wipe")
	}
}

func TestStore_WipesWhenUserFileGone(t *testing.T) {
	home := withTempHome(t)
	if err := os.MkdirAll(filepath.Join(home, ".config", "ctm"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".config", "ctm", "user.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := auth.NewStore()
	tok, _ := s.Create("alice")
	s.SetStaleWindowForTest(0)
	if _, ok := s.Lookup(tok); !ok {
		t.Fatal("unexpected: lookup failed with file present")
	}
	_ = os.Remove(filepath.Join(home, ".config", "ctm", "user.json"))
	if _, ok := s.Lookup(tok); ok {
		t.Fatal("Lookup succeeded after user.json deleted")
	}
}

func TestLookup_ExpiredReturnsFalse(t *testing.T) {
	withTempHome(t)
	s := auth.NewStoreWithTTL(time.Second)
	base := time.Unix(1_700_000_000, 0)
	s.SetClockForTest(func() time.Time { return base })
	tok, err := s.Create("alice")
	if err != nil {
		t.Fatal(err)
	}
	// Advance past the TTL.
	s.SetClockForTest(func() time.Time { return base.Add(2 * time.Second) })
	if user, ok := s.Lookup(tok); ok {
		t.Fatalf("Lookup after TTL expiry = (%q, true), want (\"\", false)", user)
	}
}

func TestLookup_WithinTTLReturnsTrue(t *testing.T) {
	withTempHome(t)
	s := auth.NewStoreWithTTL(time.Minute)
	base := time.Unix(1_700_000_000, 0)
	s.SetClockForTest(func() time.Time { return base })
	tok, err := s.Create("alice")
	if err != nil {
		t.Fatal(err)
	}
	// Advance by less than the TTL.
	s.SetClockForTest(func() time.Time { return base.Add(30 * time.Second) })
	user, ok := s.Lookup(tok)
	if !ok || user != "alice" {
		t.Fatalf("Lookup within TTL = (%q, %v), want (\"alice\", true)", user, ok)
	}
}

func TestExpiredTokenIsEvicted(t *testing.T) {
	withTempHome(t)
	s := auth.NewStoreWithTTL(time.Second)
	base := time.Unix(1_700_000_000, 0)
	s.SetClockForTest(func() time.Time { return base })
	tok, err := s.Create("alice")
	if err != nil {
		t.Fatal(err)
	}
	if got := s.EntryCountForTest(); got != 1 {
		t.Fatalf("pre-expiry entry count = %d, want 1", got)
	}
	s.SetClockForTest(func() time.Time { return base.Add(2 * time.Second) })
	if _, ok := s.Lookup(tok); ok {
		t.Fatal("expired Lookup returned ok=true")
	}
	if got := s.EntryCountForTest(); got != 0 {
		t.Fatalf("post-expiry entry count = %d, want 0 (expected lazy eviction)", got)
	}
	// Second lookup should still report false.
	if _, ok := s.Lookup(tok); ok {
		t.Fatal("second Lookup of expired token returned ok=true")
	}
}

func TestStore_Concurrent(t *testing.T) {
	withTempHome(t)
	s := auth.NewStore()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, err := s.Create("alice")
			if err != nil {
				t.Error(err)
				return
			}
			if _, ok := s.Lookup(tok); !ok {
				t.Error("created token not found on immediate lookup")
			}
			s.Revoke(tok)
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent ops did not finish in 5s — possible deadlock")
	}
}
