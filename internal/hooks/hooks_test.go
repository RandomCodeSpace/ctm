package hooks

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRun_NoEntryIsNoOp(t *testing.T) {
	// Empty map: nothing to run.
	if err := Run(EventOnAttach, nil, Context{}, 0); err != nil {
		t.Errorf("empty map should be no-op, got: %v", err)
	}
	// Map without this event: nothing to run.
	if err := Run(EventOnAttach, map[string]string{EventOnKill: "true"}, Context{}, 0); err != nil {
		t.Errorf("missing event entry should be no-op, got: %v", err)
	}
	// Whitespace-only command: no-op.
	if err := Run(EventOnAttach, map[string]string{EventOnAttach: "   "}, Context{}, 0); err != nil {
		t.Errorf("whitespace command should be no-op, got: %v", err)
	}
}

func TestRun_UnknownEventLogsAndNoOps(t *testing.T) {
	err := Run("on_bogus", map[string]string{"on_bogus": "true"}, Context{}, 0)
	if err != nil {
		t.Errorf("unknown event should no-op, got: %v", err)
	}
}

func TestRun_SuccessfulCommand(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "out.txt")

	cmd := "echo hello > " + out
	err := Run(EventOnAttach,
		map[string]string{EventOnAttach: cmd},
		Context{SessionName: "alpha"},
		0)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != "hello\n" {
		t.Errorf("expected 'hello\\n', got %q", data)
	}
}

func TestRun_EnvVarsExposed(t *testing.T) {
	dir := t.TempDir()
	out := filepath.Join(dir, "env.txt")

	cmd := `printf "name=%s mode=%s workdir=%s event=%s" "$CTM_SESSION_NAME" "$CTM_SESSION_MODE" "$CTM_SESSION_WORKDIR" "$CTM_EVENT" > ` + out
	err := Run(EventOnYolo,
		map[string]string{EventOnYolo: cmd},
		Context{SessionName: "alpha", SessionMode: "yolo", SessionWorkdir: "/w"},
		0)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	data, _ := os.ReadFile(out)
	want := "name=alpha mode=yolo workdir=/w event=on_yolo"
	if string(data) != want {
		t.Errorf("env vars wrong:\nwant: %s\ngot:  %s", want, data)
	}
}

func TestRun_NonZeroExitReturnsError(t *testing.T) {
	err := Run(EventOnKill,
		map[string]string{EventOnKill: "exit 3"},
		Context{},
		0)
	if err == nil {
		t.Error("expected error from exit 3, got nil")
	}
}

func TestRun_TimeoutKillsCommand(t *testing.T) {
	start := time.Now()
	err := Run(EventOnAttach,
		map[string]string{EventOnAttach: "sleep 10"},
		Context{},
		200*time.Millisecond)
	elapsed := time.Since(start)
	if err == nil {
		t.Error("expected timeout error, got nil")
	}
	if elapsed > 2*time.Second {
		t.Errorf("timeout was not enforced: elapsed %v, expected <2s", elapsed)
	}
}

func TestIsKnownEvent(t *testing.T) {
	for _, good := range []string{EventOnAttach, EventOnNew, EventOnYolo, EventOnSafe, EventOnKill} {
		if !IsKnownEvent(good) {
			t.Errorf("%s should be known", good)
		}
	}
	for _, bad := range []string{"", "attach", "onattach", "ON_ATTACH"} {
		if IsKnownEvent(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}
