package session_test

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/session"
)

// TestStoreUpdateHealth covers the happy path: the session's
// LastHealthStatus + LastHealthAt are stamped, persisted, and visible
// via Get on the next call.
func TestStoreUpdateHealth(t *testing.T) {
	st := newStore(t)
	if err := st.Save(session.New("alpha", "/work", "safe")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	before := time.Now().UTC().Add(-time.Second)
	if err := st.UpdateHealth("alpha", "ok"); err != nil {
		t.Fatalf("UpdateHealth: %v", err)
	}
	got, err := st.Get("alpha")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LastHealthStatus != "ok" {
		t.Errorf("LastHealthStatus = %q, want ok", got.LastHealthStatus)
	}
	if got.LastHealthAt.Before(before) {
		t.Errorf("LastHealthAt = %v, want >= %v", got.LastHealthAt, before)
	}
	// Subsequent update overwrites.
	if err := st.UpdateHealth("alpha", "degraded"); err != nil {
		t.Fatalf("UpdateHealth degraded: %v", err)
	}
	got2, _ := st.Get("alpha")
	if got2.LastHealthStatus != "degraded" {
		t.Errorf("LastHealthStatus after second update = %q, want degraded", got2.LastHealthStatus)
	}
	if !got2.LastHealthAt.After(got.LastHealthAt) && !got2.LastHealthAt.Equal(got.LastHealthAt) {
		// Within the same wall-clock tick they may compare equal; only
		// fail if we somehow went backwards.
		t.Errorf("LastHealthAt regressed: %v < %v", got2.LastHealthAt, got.LastHealthAt)
	}
}

// TestStoreUpdateHealth_Missing exercises the "session not found" branch.
func TestStoreUpdateHealth_Missing(t *testing.T) {
	st := newStore(t)
	if err := st.UpdateHealth("ghost", "ok"); err == nil {
		t.Error("expected error updating health on missing session")
	}
}

// TestStoreUpdateAttached covers the happy path: LastAttachedAt is
// stamped to a non-zero, recent UTC timestamp.
func TestStoreUpdateAttached(t *testing.T) {
	st := newStore(t)
	if err := st.Save(session.New("alpha", "/work", "safe")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	before := time.Now().UTC().Add(-time.Second)
	if err := st.UpdateAttached("alpha"); err != nil {
		t.Fatalf("UpdateAttached: %v", err)
	}
	got, err := st.Get("alpha")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.LastAttachedAt.IsZero() {
		t.Error("LastAttachedAt is zero, want non-zero after UpdateAttached")
	}
	if got.LastAttachedAt.Before(before) {
		t.Errorf("LastAttachedAt = %v, want >= %v", got.LastAttachedAt, before)
	}
}

// TestStoreUpdateAttached_Missing exercises the "session not found" branch.
func TestStoreUpdateAttached_Missing(t *testing.T) {
	st := newStore(t)
	if err := st.UpdateAttached("ghost"); err == nil {
		t.Error("expected error updating attached on missing session")
	}
}

// TestStoreUpdateMode_Missing covers the not-found branch in UpdateMode
// (UpdateMode happy path is already covered by TestStoreUpdateMode).
func TestStoreUpdateMode_Missing(t *testing.T) {
	st := newStore(t)
	if err := st.UpdateMode("ghost", "yolo"); err == nil {
		t.Error("expected error updating mode on missing session")
	}
}

// TestStoreNames_Empty + Populated covers the Names() exposure used by
// shell completions.
func TestStoreNames(t *testing.T) {
	st := newStore(t)

	// Empty store first.
	got, err := st.Names()
	if err != nil {
		t.Fatalf("Names empty: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Names empty = %v, want []", got)
	}

	// Populate.
	st.Save(session.New("alpha", "/work", "safe"))
	st.Save(session.New("beta", "/work", "safe"))
	st.Save(session.New("gamma", "/work", "yolo"))

	got, err = st.Names()
	if err != nil {
		t.Fatalf("Names: %v", err)
	}
	sort.Strings(got)
	want := []string{"alpha", "beta", "gamma"}
	if len(got) != len(want) {
		t.Fatalf("Names len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i, n := range want {
		if got[i] != n {
			t.Errorf("Names[%d] = %q, want %q", i, got[i], n)
		}
	}
}

// TestStoreDelete_Missing covers Delete's "session not found" branch.
func TestStoreDelete_Missing(t *testing.T) {
	st := newStore(t)
	if err := st.Delete("ghost"); err == nil {
		t.Error("expected error deleting missing session")
	}
}

// TestStoreRename_Missing covers Rename's "session not found" branch.
func TestStoreRename_Missing(t *testing.T) {
	st := newStore(t)
	if err := st.Rename("ghost", "newname"); err == nil {
		t.Error("expected error renaming missing session")
	}
}

// TestStoreRename_Conflict covers the "newName already exists" branch.
func TestStoreRename_Conflict(t *testing.T) {
	st := newStore(t)
	st.Save(session.New("alpha", "/work", "safe"))
	st.Save(session.New("beta", "/work", "safe"))
	if err := st.Rename("alpha", "beta"); err == nil {
		t.Error("expected error renaming over existing session")
	}
	// Both should still exist.
	if _, err := st.Get("alpha"); err != nil {
		t.Errorf("alpha should still exist after failed rename: %v", err)
	}
	if _, err := st.Get("beta"); err != nil {
		t.Errorf("beta should still exist after failed rename: %v", err)
	}
}

// TestStoreRename_InvalidNewName covers the ValidateName failure branch.
func TestStoreRename_InvalidNewName(t *testing.T) {
	st := newStore(t)
	st.Save(session.New("alpha", "/work", "safe"))
	if err := st.Rename("alpha", "bad/name"); err == nil {
		t.Error("expected error renaming to invalid name")
	}
}

// TestStoreBackup_NoFile covers backupLocked's os.IsNotExist branch:
// before any Save() the sessions.json doesn't exist, Backup() returns
// ("", nil) — used by DeleteAll on a fresh install to skip backing up.
func TestStoreBackup_NoFile(t *testing.T) {
	st := newStore(t)
	path, err := st.Backup()
	if err != nil {
		t.Fatalf("Backup on empty store: %v", err)
	}
	if path != "" {
		t.Errorf("Backup empty store = %q, want \"\"", path)
	}
}

// TestStoreDeleteAll_NoBackupOnEmpty covers DeleteAll when there's no
// existing sessions file: backupLocked returns ("", nil) and DeleteAll
// proceeds to write the empty state. (Hits the success path of the
// `if backupPath, err := ...; err == nil && backupPath != ""` guard.)
func TestStoreDeleteAll_NoBackupOnEmpty(t *testing.T) {
	st := newStore(t)
	if err := st.DeleteAll(); err != nil {
		t.Fatalf("DeleteAll on empty store: %v", err)
	}
	// Result should still be a usable empty store.
	list, err := st.List()
	if err != nil {
		t.Fatalf("List after DeleteAll: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("expected 0 sessions after DeleteAll on empty store, got %d", len(list))
	}
}

// TestStoreDeleteAll_BacksUpExisting confirms DeleteAll creates a
// .bak.<timestamp> file when sessions.json existed prior. This makes
// the backupLocked-success branch in DeleteAll observable.
func TestStoreDeleteAll_BacksUpExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sessions.json")
	st := session.NewStore(path)

	if err := st.Save(session.New("alpha", "/work", "safe")); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if err := st.DeleteAll(); err != nil {
		t.Fatalf("DeleteAll: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if filepath.Ext(e.Name()) == "" {
			continue
		}
		// Look for a sibling with .bak. in its name.
		if name := e.Name(); len(name) > len("sessions.json.bak.") &&
			name[:len("sessions.json.bak.")] == "sessions.json.bak." {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected DeleteAll to leave a sessions.json.bak.<ts> file")
	}
}
