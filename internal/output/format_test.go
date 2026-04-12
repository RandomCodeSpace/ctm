package output

import (
	"bytes"
	"strings"
	"testing"
)

func TestSuccess(t *testing.T) {
	var buf bytes.Buffer
	p := NewPrinter(&buf)
	p.Success("Session created: %s", "myproject")
	got := buf.String()
	if got == "" {
		t.Fatal("expected non-empty output")
	}
	if !containsText(got, "Session created: myproject") {
		t.Errorf("expected message in output, got: %q", got)
	}
}

func TestError(t *testing.T) {
	var buf bytes.Buffer
	p := NewPrinter(&buf)
	p.Error("cannot attach", "claude process is dead", "run 'ctm forget myproject'")
	got := buf.String()
	if !containsText(got, "cannot attach") {
		t.Errorf("expected 'cannot attach' in output, got: %q", got)
	}
	if !containsText(got, "claude process is dead") {
		t.Errorf("expected reason in output, got: %q", got)
	}
	if !containsText(got, "ctm forget myproject") {
		t.Errorf("expected fix in output, got: %q", got)
	}
}

func TestWarn(t *testing.T) {
	var buf bytes.Buffer
	p := NewPrinter(&buf)
	p.Warn("Not a git repo - no checkpoint")
	got := buf.String()
	if !containsText(got, "Not a git repo") {
		t.Errorf("expected warning text, got: %q", got)
	}
}

func TestInfo(t *testing.T) {
	var buf bytes.Buffer
	p := NewPrinter(&buf)
	p.Info("Attaching to: %s", "claude")
	got := buf.String()
	if !containsText(got, "Attaching to: claude") {
		t.Errorf("expected info text, got: %q", got)
	}
}

func TestDebug_Verbose(t *testing.T) {
	var buf bytes.Buffer
	p := NewPrinter(&buf)
	p.Debug(true, "test message %s", "here")
	if !containsText(buf.String(), "[debug] test message here") {
		t.Errorf("expected debug output, got: %q", buf.String())
	}
}

func TestDebug_Silent(t *testing.T) {
	var buf bytes.Buffer
	p := NewPrinter(&buf)
	p.Debug(false, "hidden message")
	if buf.String() != "" {
		t.Error("expected no output when verbose=false")
	}
}

// containsText strips ANSI codes and checks for substring
func containsText(s, substr string) bool {
	clean := stripANSI(s)
	return len(clean) > 0 && strings.Contains(clean, substr)
}
