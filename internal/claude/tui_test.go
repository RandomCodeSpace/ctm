package claude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadEffortLevel(t *testing.T) {
	tests := []struct {
		name    string
		initial string // "" = don't create file
		want    string
	}{
		{"missing file", "", ""},
		{"key absent", `{"theme":"dark"}`, ""},
		{"string value", `{"effortLevel":"xhigh","theme":"dark"}`, "xhigh"},
		{"empty string", `{"effortLevel":""}`, ""},
		{"wrong type (int)", `{"effortLevel":3}`, ""},
		{"wrong type (null)", `{"effortLevel":null}`, ""},
		{"malformed json", `{not json`, ""},
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
			if got := ReadEffortLevel(path); got != tt.want {
				t.Errorf("ReadEffortLevel = %q, want %q", got, tt.want)
			}
		})
	}
}
