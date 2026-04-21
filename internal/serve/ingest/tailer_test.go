package ingest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/serve/events"
)

const tailerTestUUID = "11111111-2222-3333-4444-555555555555"

func writeLine(t *testing.T, path string, payload map[string]any) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer f.Close()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if _, err := f.Write(append(body, '\n')); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func awaitEvent(t *testing.T, sub *events.Sub, want time.Duration) (events.Event, bool) {
	t.Helper()
	select {
	case e, ok := <-sub.Events():
		return e, ok
	case <-time.After(want):
		return events.Event{}, false
	}
}

func TestTailer_PublishesAppendedLines(t *testing.T) {
	dir := t.TempDir()
	hub := events.NewHub(0)
	sub, _ := hub.Subscribe("alpha", "")
	defer sub.Close()

	tail := NewTailer("alpha", tailerTestUUID, dir, hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = tail.Run(ctx) }()

	// Give the watcher a beat to register.
	time.Sleep(50 * time.Millisecond)

	logPath := filepath.Join(dir, tailerTestUUID+".jsonl")
	writeLine(t, logPath, map[string]any{
		"tool_name": "Bash",
		"tool_input": map[string]any{
			"command": "go test ./...",
		},
		"tool_response": map[string]any{
			"output":   "ok\nPASS\n",
			"is_error": false,
		},
		"ctm_timestamp": "2026-04-20T15:30:42Z",
	})

	ev, ok := awaitEvent(t, sub, 2*time.Second)
	if !ok {
		t.Fatal("no tool_call event published in 2s")
	}
	if ev.Type != "tool_call" {
		t.Errorf("ev.Type = %q, want tool_call", ev.Type)
	}
	if ev.Session != "alpha" {
		t.Errorf("ev.Session = %q, want alpha", ev.Session)
	}
	var p ToolCallPayload
	if err := json.Unmarshal(ev.Payload, &p); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if p.Tool != "Bash" {
		t.Errorf("Tool = %q, want Bash", p.Tool)
	}
	if p.Input != "go test ./..." {
		t.Errorf("Input = %q, want %q", p.Input, "go test ./...")
	}
	if p.IsError {
		t.Error("IsError = true, want false")
	}
	if !p.TS.Equal(time.Date(2026, 4, 20, 15, 30, 42, 0, time.UTC)) {
		t.Errorf("TS = %v, want 2026-04-20T15:30:42Z", p.TS)
	}
}

func TestTailer_HandlesPreexistingFileOnStartup(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, tailerTestUUID+".jsonl")
	// Pre-seed with two lines BEFORE starting the tailer.
	for _, cmd := range []string{"ls", "pwd"} {
		writeLine(t, logPath, map[string]any{
			"tool_name":  "Bash",
			"tool_input": map[string]any{"command": cmd},
		})
	}

	hub := events.NewHub(0)
	sub, _ := hub.Subscribe("a", "")
	defer sub.Close()

	tail := NewTailer("a", tailerTestUUID, dir, hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = tail.Run(ctx) }()

	// Both pre-existing lines should be drained.
	for i, want := range []string{"ls", "pwd"} {
		ev, ok := awaitEvent(t, sub, 2*time.Second)
		if !ok {
			t.Fatalf("event %d: timeout", i)
		}
		var p ToolCallPayload
		_ = json.Unmarshal(ev.Payload, &p)
		if p.Input != want {
			t.Errorf("event %d: Input = %q, want %q", i, p.Input, want)
		}
	}
}

func TestTailer_SkipsMalformedLines(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, tailerTestUUID+".jsonl")

	hub := events.NewHub(0)
	sub, _ := hub.Subscribe("a", "")
	defer sub.Close()

	tail := NewTailer("a", tailerTestUUID, dir, hub)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = tail.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)

	// Mixed write: one valid + one corrupt + one valid.
	f, err := os.OpenFile(logPath, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	body, _ := json.Marshal(map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": "first"},
	})
	if _, err := f.Write(append(body, '\n')); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	if _, err := f.Write([]byte("not-json{\n")); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	body2, _ := json.Marshal(map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]any{"command": "second"},
	})
	if _, err := f.Write(append(body2, '\n')); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	_ = f.Close()

	for _, want := range []string{"first", "second"} {
		ev, ok := awaitEvent(t, sub, 2*time.Second)
		if !ok {
			t.Fatalf("missing event for %q", want)
		}
		var p ToolCallPayload
		_ = json.Unmarshal(ev.Payload, &p)
		if p.Input != want {
			t.Errorf("got %q, want %q", p.Input, want)
		}
	}
}

func TestTailerManager_StartIdempotentAndStop(t *testing.T) {
	dir := t.TempDir()
	hub := events.NewHub(0)
	mgr := NewTailerManager(dir, hub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr.Start(ctx, "alpha", tailerTestUUID)
	mgr.Start(ctx, "alpha", tailerTestUUID) // idempotent
	if got := mgr.Active(); len(got) != 1 || got[0] != "alpha" {
		t.Errorf("Active = %v, want [alpha]", got)
	}

	mgr.Stop("alpha")
	if got := mgr.Active(); len(got) != 0 {
		t.Errorf("Active after Stop = %v, want []", got)
	}
}

func TestTailerManager_StopAll(t *testing.T) {
	dir := t.TempDir()
	hub := events.NewHub(0)
	mgr := NewTailerManager(dir, hub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr.Start(ctx, "a", "uuid-a")
	mgr.Start(ctx, "b", "uuid-b")
	mgr.Start(ctx, "c", "uuid-c")
	if got := len(mgr.Active()); got != 3 {
		t.Fatalf("Active count = %d, want 3", got)
	}
	mgr.StopAll()
	if got := len(mgr.Active()); got != 0 {
		t.Errorf("Active after StopAll = %d, want 0", got)
	}
}

func TestSummariseInput(t *testing.T) {
	tests := []struct {
		tool string
		in   map[string]any
		want string
	}{
		{"Bash", map[string]any{"command": "ls -la"}, "ls -la"},
		{"Edit", map[string]any{"file_path": "/tmp/foo.go"}, "/tmp/foo.go"},
		{"Read", map[string]any{"file_path": "/tmp/bar.txt"}, "/tmp/bar.txt"},
		{"Glob", map[string]any{"pattern": "*.go"}, "*.go"},
	}
	for _, tt := range tests {
		raw := map[string]any{"tool_name": tt.tool, "tool_input": tt.in}
		got := summariseInput(raw)
		if got != tt.want {
			t.Errorf("summariseInput(%s) = %q, want %q", tt.tool, got, tt.want)
		}
	}
}

func TestTruncate_LongStringClipped(t *testing.T) {
	long := make([]byte, 500)
	for i := range long {
		long[i] = 'a'
	}
	got := truncate(string(long))
	if len(got) != inputSummaryMax {
		t.Errorf("len = %d, want %d", len(got), inputSummaryMax)
	}
	if got[len(got)-1] != []byte("…")[2] {
		// Last byte is the third byte of the U+2026 encoding; just
		// assert the truncation marker is present.
		if !endsWithEllipsis(got) {
			t.Errorf("missing ellipsis: %q", got[len(got)-3:])
		}
	}
}

func endsWithEllipsis(s string) bool {
	const e = "…"
	if len(s) < len(e) {
		return false
	}
	return s[len(s)-len(e):] == e
}
