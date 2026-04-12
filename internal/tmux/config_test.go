package tmux

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tmux.conf")
	err := GenerateConfig(path, 50000)
	if err != nil {
		t.Fatalf("GenerateConfig failed: %v", err)
	}
	data, _ := os.ReadFile(path)
	content := string(data)
	for _, want := range []string{
		"set -g mouse on",
		"set -g history-limit 50000",
		"set -g status-position top",
		"set -sg escape-time 10",
		"set -g set-clipboard on",
		"set -g prefix2 M-a",
		"bind -n M-[ copy-mode",
		"bind -n M-d detach-client",
		`set -g default-terminal "tmux-256color"`,
	} {
		if !strings.Contains(content, want) {
			t.Errorf("config missing %q", want)
		}
	}
}

func TestGenerateConfigCustomScrollback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tmux.conf")
	GenerateConfig(path, 10000)
	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "set -g history-limit 10000") {
		t.Error("expected custom scrollback value")
	}
}
