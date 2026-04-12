package shell

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateFromCC_NoDir(t *testing.T) {
	sessionsPath := filepath.Join(t.TempDir(), "sessions.json")
	migrated, err := MigrateFromCC("/tmp/ctm-nonexistent-xyz", sessionsPath)
	if err != nil {
		t.Fatalf("expected no error for missing dir, got: %v", err)
	}
	if len(migrated) != 0 {
		t.Errorf("expected 0 migrated, got %d", len(migrated))
	}
}

func TestMigrateFromCC_WithSessions(t *testing.T) {
	ccDir := t.TempDir()
	sessionsPath := filepath.Join(t.TempDir(), "sessions.json")

	// Create two session files with UUIDs
	sessions := map[string]string{
		"myproject":  "550e8400-e29b-41d4-a716-446655440000",
		"worksprint": "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
	}
	for name, uuid := range sessions {
		if err := os.WriteFile(filepath.Join(ccDir, name), []byte(uuid+"\n"), 0644); err != nil {
			t.Fatalf("write session file: %v", err)
		}
	}

	migrated, err := MigrateFromCC(ccDir, sessionsPath)
	if err != nil {
		t.Fatalf("MigrateFromCC: %v", err)
	}
	if len(migrated) != 2 {
		t.Errorf("expected 2 migrated, got %d: %v", len(migrated), migrated)
	}
}

func TestMigrateFromCC_Idempotent(t *testing.T) {
	ccDir := t.TempDir()
	sessionsPath := filepath.Join(t.TempDir(), "sessions.json")

	uuid := "550e8400-e29b-41d4-a716-446655440000"
	if err := os.WriteFile(filepath.Join(ccDir, "myproject"), []byte(uuid), 0644); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	first, err := MigrateFromCC(ccDir, sessionsPath)
	if err != nil {
		t.Fatalf("first MigrateFromCC: %v", err)
	}
	if len(first) != 1 {
		t.Errorf("expected 1 migrated on first run, got %d", len(first))
	}

	second, err := MigrateFromCC(ccDir, sessionsPath)
	if err != nil {
		t.Fatalf("second MigrateFromCC: %v", err)
	}
	if len(second) != 0 {
		t.Errorf("expected 0 migrated on second run (idempotent), got %d", len(second))
	}
}
