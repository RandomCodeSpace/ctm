package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildCommandSafeNew(t *testing.T) {
	cmd := BuildCommand("abc-123", "safe", false, "")
	expected := "claude --session-id abc-123"
	if cmd != expected {
		t.Errorf("got: %s, want: %s", cmd, expected)
	}
}

func TestBuildCommandYoloNew(t *testing.T) {
	cmd := BuildCommand("abc-123", "yolo", false, "")
	expected := "claude --session-id abc-123 --dangerously-skip-permissions"
	if cmd != expected {
		t.Errorf("got: %s, want: %s", cmd, expected)
	}
}

func TestBuildCommandResume(t *testing.T) {
	cmd := BuildCommand("abc-123", "safe", true, "")
	if !strings.Contains(cmd, "--resume abc-123") {
		t.Errorf("expected --resume abc-123, got: %s", cmd)
	}
	if !strings.Contains(cmd, "|| claude --session-id abc-123") {
		t.Errorf("expected fallback to --session-id, got: %s", cmd)
	}
}

func TestBuildCommandYoloResume(t *testing.T) {
	cmd := BuildCommand("abc-123", "yolo", true, "")
	if !strings.Contains(cmd, "--resume abc-123 --dangerously-skip-permissions") {
		t.Errorf("expected --resume with yolo flag, got: %s", cmd)
	}
	if !strings.Contains(cmd, "|| claude --session-id abc-123 --dangerously-skip-permissions") {
		t.Errorf("expected fallback with yolo flag, got: %s", cmd)
	}
}

func TestBuildCommandWithOverlay(t *testing.T) {
	cmd := BuildCommand("abc-123", "safe", false, "/home/u/.config/ctm/claude-overlay.json")
	// New if/else form: re-checks file at exec time, never word-splits the path.
	if !strings.HasPrefix(cmd, "if [ -r '/home/u/.config/ctm/claude-overlay.json' ]; then ") {
		t.Errorf("expected if-test prefix, got: %s", cmd)
	}
	if !strings.Contains(cmd, "claude --session-id abc-123 --settings '/home/u/.config/ctm/claude-overlay.json'") {
		t.Errorf("expected then-branch with settings, got: %s", cmd)
	}
	if !strings.Contains(cmd, "; else claude --session-id abc-123; fi") {
		t.Errorf("expected else-branch without settings, got: %s", cmd)
	}
}

func TestBuildCommandWithOverlayResume(t *testing.T) {
	cmd := BuildCommand("abc-123", "yolo", true, "/tmp/overlay.json")
	// Verify if-test wraps both branches and yolo flag is preserved
	if !strings.HasPrefix(cmd, "if [ -r '/tmp/overlay.json' ]; then ") {
		t.Errorf("expected if-test prefix, got: %s", cmd)
	}
	// Then-branch: --resume + overlay || --session-id + overlay (both with yolo)
	if !strings.Contains(cmd, "claude --resume abc-123 --dangerously-skip-permissions --settings '/tmp/overlay.json' || claude --session-id abc-123 --dangerously-skip-permissions --settings '/tmp/overlay.json'") {
		t.Errorf("then-branch missing or wrong: %s", cmd)
	}
	// Else-branch: same fallback without overlay
	if !strings.Contains(cmd, "; else claude --resume abc-123 --dangerously-skip-permissions || claude --session-id abc-123 --dangerously-skip-permissions; fi") {
		t.Errorf("else-branch missing or wrong: %s", cmd)
	}
}

func TestBuildCommandWithOverlayPathContainsSpaces(t *testing.T) {
	// Real-world scenario: $HOME has a space in it
	cmd := BuildCommand("abc-123", "safe", false, "/home/My User/.config/ctm/claude-overlay.json")
	// The path must appear as a single quoted token in BOTH the test
	// and the --settings argument — never word-split.
	if !strings.Contains(cmd, "[ -r '/home/My User/.config/ctm/claude-overlay.json' ]") {
		t.Errorf("path with spaces lost quoting in test: %s", cmd)
	}
	if !strings.Contains(cmd, "--settings '/home/My User/.config/ctm/claude-overlay.json'") {
		t.Errorf("path with spaces lost quoting in --settings: %s", cmd)
	}
}

func TestShellQuote(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"/simple/path", "'/simple/path'"},
		{"/path with spaces", "'/path with spaces'"},
		{"/path/with'quote", `'/path/with'\''quote'`},
	}
	for _, tt := range tests {
		got := shellQuote(tt.in)
		if got != tt.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestOverlayPathIfExists(t *testing.T) {
	dir := t.TempDir()

	t.Run("empty path returns empty", func(t *testing.T) {
		if got := OverlayPathIfExists(""); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("missing file returns empty", func(t *testing.T) {
		if got := OverlayPathIfExists(filepath.Join(dir, "nope.json")); got != "" {
			t.Errorf("got %q, want empty", got)
		}
	})

	t.Run("existing file returns path", func(t *testing.T) {
		path := filepath.Join(dir, "exists.json")
		if err := os.WriteFile(path, []byte("{}"), 0644); err != nil {
			t.Fatal(err)
		}
		if got := OverlayPathIfExists(path); got != path {
			t.Errorf("got %q, want %q", got, path)
		}
	})
}
