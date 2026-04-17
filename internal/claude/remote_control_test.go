package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureRemoteControlAtStartup(t *testing.T) {
	tests := []struct {
		name       string
		initial    string // "" means file does not exist
		wantExists bool
		// wantKey: "" means key must be absent from the result,
		// otherwise the JSON-encoded value ("true" / "false").
		wantKey string
	}{
		{
			name:       "missing file = no-op (do not create)",
			initial:    "",
			wantExists: false,
			wantKey:    "",
		},
		{
			name:       "key absent = added as true",
			initial:    `{"foo":"bar"}`,
			wantExists: true,
			wantKey:    "true",
		},
		{
			name:       "key already true = left alone",
			initial:    `{"remoteControlAtStartup":true,"foo":"bar"}`,
			wantExists: true,
			wantKey:    "true",
		},
		{
			name:       "key false = respected (user opted out)",
			initial:    `{"remoteControlAtStartup":false,"foo":"bar"}`,
			wantExists: true,
			wantKey:    "false",
		},
		{
			name:       "empty JSON object = key added",
			initial:    `{}`,
			wantExists: true,
			wantKey:    "true",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".claude.json")

			if tt.initial != "" {
				if err := os.WriteFile(path, []byte(tt.initial), 0600); err != nil {
					t.Fatalf("setup: %v", err)
				}
			}

			if err := EnsureRemoteControlAtStartup(path); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			_, err := os.Stat(path)
			exists := err == nil
			if exists != tt.wantExists {
				t.Fatalf("file exists = %v, want %v", exists, tt.wantExists)
			}
			if !exists {
				return
			}

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("reading result: %v", err)
			}
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(data, &obj); err != nil {
				t.Fatalf("parsing result: %v", err)
			}

			got, present := obj["remoteControlAtStartup"]
			switch {
			case tt.wantKey == "" && present:
				t.Errorf("key unexpectedly present = %s", string(got))
			case tt.wantKey != "" && !present:
				t.Errorf("key missing, want %q", tt.wantKey)
			case tt.wantKey != "" && string(got) != tt.wantKey:
				t.Errorf("value = %s, want %s", string(got), tt.wantKey)
			}

			if raw, ok := obj["foo"]; ok {
				if string(raw) != `"bar"` {
					t.Errorf("sibling key foo = %s, want \"bar\"", string(raw))
				}
			}
		})
	}
}

func TestEnsureRemoteControlAtStartup_PreservesFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := EnsureRemoteControlAtStartup(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("mode = %v, want 0600", mode)
	}
}

func TestEnsureRemoteControlAtStartup_InvalidJSONReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	if err := os.WriteFile(path, []byte(`{not json`), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := EnsureRemoteControlAtStartup(path); err == nil {
		t.Fatal("expected error on invalid JSON, got nil")
	}

	// Ensure we didn't clobber the user's (broken) file.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading after error: %v", err)
	}
	if string(data) != `{not json` {
		t.Errorf("original file modified on parse error: %q", string(data))
	}
}

func TestEnsureRemoteControlAtStartup_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".claude.json")
	if err := os.WriteFile(path, []byte(`{"foo":"bar"}`), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// First run adds the key.
	if err := EnsureRemoteControlAtStartup(path); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read 1: %v", err)
	}

	// Second run must not change the file (key already present).
	if err := EnsureRemoteControlAtStartup(path); err != nil {
		t.Fatalf("run 2: %v", err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read 2: %v", err)
	}

	if string(first) != string(second) {
		t.Errorf("second run modified file:\nfirst:  %s\nsecond: %s", first, second)
	}
}
