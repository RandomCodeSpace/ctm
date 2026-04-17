package claude

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureTUIFullscreen(t *testing.T) {
	tests := []struct {
		name       string
		initial    string
		wantExists bool
		// wantTUI: "" means key must be absent from result;
		// otherwise the JSON-encoded string value (including quotes).
		wantTUI string
	}{
		{
			name:       "missing file = no-op (do not create)",
			initial:    "",
			wantExists: false,
			wantTUI:    "",
		},
		{
			name:       "key absent = set to fullscreen",
			initial:    `{"theme":"dark"}`,
			wantExists: true,
			wantTUI:    `"fullscreen"`,
		},
		{
			name:       `key "default" = upgraded to fullscreen`,
			initial:    `{"tui":"default","theme":"dark"}`,
			wantExists: true,
			wantTUI:    `"fullscreen"`,
		},
		{
			name:       `key already "fullscreen" = left alone`,
			initial:    `{"tui":"fullscreen"}`,
			wantExists: true,
			wantTUI:    `"fullscreen"`,
		},
		{
			name:       `key "compact" = respected (user opted out of fullscreen)`,
			initial:    `{"tui":"compact"}`,
			wantExists: true,
			wantTUI:    `"compact"`,
		},
		{
			name:       `key "custom-mode" = respected (unknown explicit choice)`,
			initial:    `{"tui":"custom-mode"}`,
			wantExists: true,
			wantTUI:    `"custom-mode"`,
		},
		{
			name:       "empty JSON object = key added",
			initial:    `{}`,
			wantExists: true,
			wantTUI:    `"fullscreen"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "settings.json")

			if tt.initial != "" {
				if err := os.WriteFile(path, []byte(tt.initial), 0644); err != nil {
					t.Fatalf("setup: %v", err)
				}
			}

			if err := EnsureTUIFullscreen(path); err != nil {
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

			got, present := obj["tui"]
			switch {
			case tt.wantTUI == "" && present:
				t.Errorf("tui unexpectedly present = %s", string(got))
			case tt.wantTUI != "" && !present:
				t.Errorf("tui missing, want %s", tt.wantTUI)
			case tt.wantTUI != "" && string(got) != tt.wantTUI:
				t.Errorf("tui = %s, want %s", string(got), tt.wantTUI)
			}

			if raw, ok := obj["theme"]; ok {
				if string(raw) != `"dark"` {
					t.Errorf("sibling key theme = %s, want \"dark\"", string(raw))
				}
			}
		})
	}
}

func TestEnsureTUIFullscreen_PreservesFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{}`), 0640); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := EnsureTUIFullscreen(path); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0640 {
		t.Errorf("mode = %v, want 0640", mode)
	}
}

func TestEnsureTUIFullscreen_InvalidJSONReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	original := `{not json`
	if err := os.WriteFile(path, []byte(original), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := EnsureTUIFullscreen(path); err == nil {
		t.Fatal("expected error on invalid JSON, got nil")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading after error: %v", err)
	}
	if string(data) != original {
		t.Errorf("original file modified on parse error: %q", string(data))
	}
}

func TestEnsureTUIFullscreen_Idempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(path, []byte(`{"theme":"dark"}`), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if err := EnsureTUIFullscreen(path); err != nil {
		t.Fatalf("run 1: %v", err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read 1: %v", err)
	}

	if err := EnsureTUIFullscreen(path); err != nil {
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
