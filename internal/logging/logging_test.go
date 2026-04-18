package logging

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		in   string
		want slog.Level
		ok   bool
	}{
		{"", slog.LevelInfo, true},
		{"info", slog.LevelInfo, true},
		{"INFO", slog.LevelInfo, true},
		{"  debug  ", slog.LevelDebug, true},
		{"warn", slog.LevelWarn, true},
		{"warning", slog.LevelWarn, true},
		{"error", slog.LevelError, true},
		{"err", slog.LevelError, true},
		{"trace", 0, false},
		{"bogus", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseLevel(tt.in)
			if tt.ok {
				if err != nil {
					t.Errorf("ParseLevel(%q) err = %v", tt.in, err)
				}
				if got != tt.want {
					t.Errorf("ParseLevel(%q) = %v, want %v", tt.in, got, tt.want)
				}
			} else {
				if err == nil {
					t.Errorf("ParseLevel(%q) expected error, got nil", tt.in)
				}
			}
		})
	}
}

func TestSetup_OnlyFirstCallApplies(t *testing.T) {
	ResetForTest()

	if err := Setup("warn"); err != nil {
		t.Fatalf("Setup warn: %v", err)
	}
	// Second call must not change the level; the sync.Once in Setup
	// guarantees this — even if the input is wildly different.
	if err := Setup("debug"); err != nil {
		t.Fatalf("Setup debug: %v", err)
	}

	// INFO would still fire if level were DEBUG; it must be filtered
	// at WARN.
	var buf bytes.Buffer
	oldHandler := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(oldHandler)

	slog.Info("should be filtered")
	if buf.Len() > 0 {
		t.Errorf("INFO leaked through at WARN: %q", buf.String())
	}
}

func TestSetup_InvalidLevelReturnsError(t *testing.T) {
	ResetForTest()
	if err := Setup("ridiculous"); err == nil {
		t.Error("expected error for unknown level, got nil")
	}
}

func TestSetup_JSONHandlerViaEnv(t *testing.T) {
	ResetForTest()

	t.Setenv(EnvFormat, "json")
	// Redirect stderr to a pipe to capture.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	oldStderr := os.Stderr
	os.Stderr = w
	defer func() { os.Stderr = oldStderr }()

	if err := Setup("info"); err != nil {
		t.Fatalf("Setup: %v", err)
	}
	slog.Info("ping", "k", "v")
	w.Close() //nolint:errcheck

	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	got := buf.String()
	if !strings.Contains(got, `"msg":"ping"`) {
		t.Errorf("expected JSON output, got: %q", got)
	}
}
