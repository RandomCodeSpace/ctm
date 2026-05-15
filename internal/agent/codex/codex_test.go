package codex_test

import (
	"strings"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/agent"
	_ "github.com/RandomCodeSpace/ctm/internal/agent/codex" // register via init
)

func TestCodex_IsRegistered(t *testing.T) {
	a, ok := agent.For("codex")
	if !ok {
		t.Fatal("codex not registered after blank import")
	}
	if a.Name() != "codex" {
		t.Fatalf("Name = %q, want codex", a.Name())
	}
	if a.ProcessName() != "codex" {
		t.Fatalf("ProcessName = %q, want codex", a.ProcessName())
	}
	if a.DefaultSessionName() != "codex" {
		t.Fatalf("DefaultSessionName = %q, want codex", a.DefaultSessionName())
	}
}

func TestCodex_Binary_DefaultsLiteral(t *testing.T) {
	t.Setenv("CTM_CODEX_BIN", "")
	a, _ := agent.For("codex")
	if got := a.Binary(); got != "codex" {
		t.Fatalf("Binary = %q, want codex", got)
	}
}

func TestCodex_Binary_HonorsEnvOverride(t *testing.T) {
	t.Setenv("CTM_CODEX_BIN", "/fake/path/to/codex")
	a, _ := agent.For("codex")
	if got := a.Binary(); got != "/fake/path/to/codex" {
		t.Fatalf("Binary = %q, want /fake/path/to/codex", got)
	}
}

func TestCodex_BuildCommand_FreshSafe(t *testing.T) {
	a, _ := agent.For("codex")
	got := a.BuildCommand(agent.SpawnSpec{Mode: "safe"})
	if got != "codex" {
		t.Fatalf("BuildCommand = %q, want codex", got)
	}
}

func TestCodex_BuildCommand_FreshYolo(t *testing.T) {
	a, _ := agent.For("codex")
	got := a.BuildCommand(agent.SpawnSpec{Mode: "yolo"})
	want := "codex --sandbox danger-full-access"
	if got != want {
		t.Fatalf("BuildCommand = %q, want %q", got, want)
	}
}

func TestCodex_BuildCommand_ResumeKnownID(t *testing.T) {
	a, _ := agent.For("codex")
	got := a.BuildCommand(agent.SpawnSpec{
		AgentSessionID: "thread-uuid-1",
		Mode:           "safe",
		Resume:         true,
	})
	if !strings.Contains(got, "codex resume 'thread-uuid-1'") {
		t.Fatalf("expected positional resume id, got: %q", got)
	}
	if !strings.HasSuffix(got, "|| codex") {
		t.Fatalf("expected fresh-codex fallback, got: %q", got)
	}
}

func TestCodex_BuildCommand_ResumeUnknownID(t *testing.T) {
	a, _ := agent.For("codex")
	got := a.BuildCommand(agent.SpawnSpec{
		Mode:   "safe",
		Resume: true,
	})
	want := "codex resume --last || codex"
	if got != want {
		t.Fatalf("BuildCommand = %q, want %q", got, want)
	}
}

func TestCodex_BuildCommand_EnvExportsPrefix(t *testing.T) {
	a, _ := agent.For("codex")
	got := a.BuildCommand(agent.SpawnSpec{
		Mode:       "safe",
		EnvExports: "export FOO='bar'",
	})
	want := "export FOO='bar'; codex"
	if got != want {
		t.Fatalf("BuildCommand = %q, want %q", got, want)
	}
}

func TestCodex_YOLOFlag(t *testing.T) {
	a, _ := agent.For("codex")
	flags := a.YOLOFlag()
	if len(flags) != 2 || flags[0] != "--sandbox" || flags[1] != "danger-full-access" {
		t.Fatalf("YOLOFlag = %v, want [--sandbox danger-full-access]", flags)
	}
}

func TestCodex_BuildCommand_ShellQuoteEmbeddedQuote(t *testing.T) {
	a, _ := agent.For("codex")
	got := a.BuildCommand(agent.SpawnSpec{
		AgentSessionID: `weird'id`,
		Mode:           "safe",
		Resume:         true,
	})
	if !strings.Contains(got, `codex resume 'weird'\''id'`) {
		t.Fatalf("expected escaped single-quote, got: %q", got)
	}
}
