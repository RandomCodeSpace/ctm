package hermes_test

import (
	"os"
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/agent"
	_ "github.com/RandomCodeSpace/ctm/internal/agent/hermes" // register via init
)

func TestRegisteredUnderHermes(t *testing.T) {
	a, ok := agent.For("hermes")
	if !ok {
		t.Fatal(`agent.For("hermes") = false; want registered`)
	}
	if a.Name() != "hermes" {
		t.Errorf("Name() = %q, want %q", a.Name(), "hermes")
	}
	if a.DefaultSessionName() != "hermes" {
		t.Errorf("DefaultSessionName() = %q, want %q", a.DefaultSessionName(), "hermes")
	}
	if a.ProcessName() != "hermes" {
		t.Errorf("ProcessName() = %q, want %q", a.ProcessName(), "hermes")
	}
}

func TestBinaryHonorsEnv(t *testing.T) {
	a, _ := agent.For("hermes")

	t.Setenv("CTM_HERMES_BIN", "/tmp/fake-hermes")
	if got := a.Binary(); got != "/tmp/fake-hermes" {
		t.Errorf("Binary() with env = %q, want %q", got, "/tmp/fake-hermes")
	}

	os.Unsetenv("CTM_HERMES_BIN")
	if got := a.Binary(); got != "hermes" {
		t.Errorf("Binary() without env = %q, want %q", got, "hermes")
	}
}

func TestYOLOFlag(t *testing.T) {
	a, _ := agent.For("hermes")
	got := a.YOLOFlag()
	if len(got) != 1 || got[0] != "--yolo" {
		t.Errorf("YOLOFlag() = %v, want [\"--yolo\"]", got)
	}
}

func TestDiscoverSessionID_missingBinaryReturnsFalse(t *testing.T) {
	t.Setenv("CTM_HERMES_BIN", "/nonexistent/path/to/hermes-xyz")
	a, _ := agent.For("hermes")
	id, ok := a.DiscoverSessionID(time.Now())
	if ok || id != "" {
		t.Errorf("DiscoverSessionID = (%q, %v), want (\"\", false)", id, ok)
	}
}
