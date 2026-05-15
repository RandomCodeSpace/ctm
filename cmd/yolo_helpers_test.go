package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/output"
	"github.com/RandomCodeSpace/ctm/internal/session"
	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

// --- decideModeAction --------------------------------------------------------

func TestDecideModeAction(t *testing.T) {
	tests := []struct {
		name          string
		sess          *session.Session
		getErr        error
		requestedMode string
		want          modeDecision
	}{
		{
			name:          "no stored session → fresh create",
			sess:          nil,
			getErr:        errors.New("not found"),
			requestedMode: "yolo",
			want:          decisionFresh,
		},
		{
			name:          "stored yolo + yolo request → resume",
			sess:          &session.Session{Mode: "yolo"},
			getErr:        nil,
			requestedMode: "yolo",
			want:          decisionResume,
		},
		{
			name:          "stored safe + safe request → resume",
			sess:          &session.Session{Mode: "safe"},
			getErr:        nil,
			requestedMode: "safe",
			want:          decisionResume,
		},
		{
			name:          "stored safe + yolo request → recreate",
			sess:          &session.Session{Mode: "safe"},
			getErr:        nil,
			requestedMode: "yolo",
			want:          decisionRecreate,
		},
		{
			name:          "stored yolo + safe request → recreate",
			sess:          &session.Session{Mode: "yolo"},
			getErr:        nil,
			requestedMode: "safe",
			want:          decisionRecreate,
		},
		{
			name:          "stored empty mode → recreate (mode mismatch)",
			sess:          &session.Session{Mode: ""},
			getErr:        nil,
			requestedMode: "yolo",
			want:          decisionRecreate,
		},
		{
			// store error wins over a non-nil sess; the function must
			// treat a lookup error as "no stored session".
			name:          "lookup error → fresh create even with sess set",
			sess:          &session.Session{Mode: "yolo"},
			getErr:        errors.New("io error"),
			requestedMode: "yolo",
			want:          decisionFresh,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := decideModeAction(tt.sess, tt.getErr, tt.requestedMode)
			if got != tt.want {
				t.Errorf("decideModeAction(%+v, %v, %q) = %d, want %d",
					tt.sess, tt.getErr, tt.requestedMode, got, tt.want)
			}
		})
	}
}

// --- bannerFor ---------------------------------------------------------------

func TestBannerFor(t *testing.T) {
	tests := []struct {
		name        string
		mode        string
		wantText    string
		wantMagenta bool
	}{
		{"yolo banner is magenta", "yolo", ">>> YOLO MODE", true},
		{"safe banner is success-green", "safe", ">>> SAFE MODE", false},
		// Defensive: any non-yolo mode falls through to safe styling.
		{"unknown mode falls back to safe styling", "weird", ">>> WEIRD MODE", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text, magenta := bannerFor(tt.mode)
			if text != tt.wantText {
				t.Errorf("bannerFor(%q) text = %q, want %q", tt.mode, text, tt.wantText)
			}
			if magenta != tt.wantMagenta {
				t.Errorf("bannerFor(%q) magenta = %v, want %v", tt.mode, magenta, tt.wantMagenta)
			}
		})
	}
}

// --- eventsFor ---------------------------------------------------------------

func TestEventsFor(t *testing.T) {
	tests := []struct {
		name           string
		mode           string
		wantHookEvent  string
		wantServeEvent string
	}{
		{
			name:           "yolo fires on_yolo to both channels",
			mode:           "yolo",
			wantHookEvent:  "on_yolo",
			wantServeEvent: "on_yolo",
		},
		{
			// Safe mode fires on_safe to user hooks but maps to
			// session_attached on the serve hub — the hub does not
			// model a separate safe lifecycle.
			name:           "safe fires on_safe to hooks but session_attached to serve",
			mode:           "safe",
			wantHookEvent:  "on_safe",
			wantServeEvent: "session_attached",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, s := eventsFor(tt.mode)
			if h != tt.wantHookEvent {
				t.Errorf("eventsFor(%q) hook = %q, want %q", tt.mode, h, tt.wantHookEvent)
			}
			if s != tt.wantServeEvent {
				t.Errorf("eventsFor(%q) serve = %q, want %q", tt.mode, s, tt.wantServeEvent)
			}
		})
	}
}

// --- resolveSimpleName -------------------------------------------------------

func TestResolveSimpleName(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"no args → default 'codex'", nil, "codex"},
		{"empty slice → default 'codex'", []string{}, "codex"},
		{"single arg → that name", []string{"my-sess"}, "my-sess"},
		{"extra args ignored — first wins", []string{"first", "ignored"}, "first"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveSimpleName(tt.args)
			if got != tt.want {
				t.Errorf("resolveSimpleName(%v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

// --- resolveModeTarget -------------------------------------------------------

// resolveModeTarget covers the runYoloBang/runSafe name+workdir block.
// We test it with an empty store and tmux client; HasSession runs
// `tmux has-session` which returns non-zero exit on missing → ok in CI.
func TestResolveModeTargetDefaultsToCwdWhenNoStoreEntry(t *testing.T) {
	tmp := t.TempDir()
	tc := tmux.NewClient("")
	store := session.NewStore(filepath.Join(tmp, "sessions.json"))

	// Use a name that is extremely unlikely to exist as a tmux session.
	name, workdir, err := resolveModeTarget([]string{"ctm-test-nonexistent-abc-9f7b"}, store, tc)
	if err != nil {
		t.Fatalf("resolveModeTarget: %v", err)
	}
	if name != "ctm-test-nonexistent-abc-9f7b" {
		t.Errorf("name = %q, want ctm-test-nonexistent-abc-9f7b", name)
	}
	cwd, _ := os.Getwd()
	if workdir != cwd {
		t.Errorf("workdir = %q, want cwd %q", workdir, cwd)
	}
}

func TestResolveModeTargetUsesStoredWorkdir(t *testing.T) {
	tmp := t.TempDir()
	storePath := filepath.Join(tmp, "sessions.json")
	store := session.NewStore(storePath)

	stored := &session.Session{
		Name:    "stored-sess",
		UUID:    "00000000-0000-0000-0000-000000000001",
		Mode:    "yolo",
		Workdir: "/tmp/somewhere",
	}
	if err := store.Save(stored); err != nil {
		t.Fatalf("Save: %v", err)
	}

	tc := tmux.NewClient("")
	name, workdir, err := resolveModeTarget([]string{"stored-sess"}, store, tc)
	if err != nil {
		t.Fatalf("resolveModeTarget: %v", err)
	}
	if name != "stored-sess" {
		t.Errorf("name = %q, want stored-sess", name)
	}
	if workdir != "/tmp/somewhere" {
		t.Errorf("workdir = %q, want /tmp/somewhere", workdir)
	}
}

func TestResolveModeTargetDefaultName(t *testing.T) {
	tmp := t.TempDir()
	tc := tmux.NewClient("")
	store := session.NewStore(filepath.Join(tmp, "sessions.json"))

	name, _, err := resolveModeTarget(nil, store, tc)
	if err != nil {
		t.Fatalf("resolveModeTarget: %v", err)
	}
	if name != "codex" {
		t.Errorf("default name = %q, want codex", name)
	}
}

// --- tearDownForRecreate -----------------------------------------------------

// When neither tmux nor store have the entry, tearDownForRecreate must
// be a no-op (no panic, no error). This covers the loud=true and
// loud=false branches of the warn-on-delete-failure logic.
func TestTearDownForRecreateNoop(t *testing.T) {
	tmp := t.TempDir()
	store := session.NewStore(filepath.Join(tmp, "sessions.json"))
	tc := tmux.NewClient("")
	out := output.NewPrinter(io_discard{})

	// Both branches: silent and loud. Neither should panic.
	tearDownForRecreate("ctm-test-nonexistent-xyz-zzzz", store, tc, out, false)
	tearDownForRecreate("ctm-test-nonexistent-xyz-zzzz", store, tc, out, true)
}

func TestTearDownForRecreateRemovesStoreEntry(t *testing.T) {
	tmp := t.TempDir()
	storePath := filepath.Join(tmp, "sessions.json")
	store := session.NewStore(storePath)

	stored := &session.Session{
		Name:    "to-be-deleted",
		UUID:    "00000000-0000-0000-0000-000000000002",
		Mode:    "yolo",
		Workdir: tmp,
	}
	if err := store.Save(stored); err != nil {
		t.Fatalf("Save: %v", err)
	}

	tc := tmux.NewClient("")
	out := output.NewPrinter(io_discard{})

	tearDownForRecreate("to-be-deleted", store, tc, out, true)

	if _, err := store.Get("to-be-deleted"); err == nil {
		t.Errorf("expected store entry deleted, but Get succeeded")
	}
}

// --- fireLaunchEvents --------------------------------------------------------

// fireLaunchEvents reads config (returns err with empty HOME → fireHook
// noop) and posts to /api/hooks/:event (silent fail when serve is down).
// The test verifies it doesn't panic and tolerates a missing config.
func TestFireLaunchEventsNoConfigNoPanic(t *testing.T) {
	withTempHome(t)
	tmp := t.TempDir()
	store := session.NewStore(filepath.Join(tmp, "sessions.json"))

	// Both modes — covers eventsFor branches end-to-end.
	fireLaunchEvents(store, "ephemeral-yolo", "/tmp/x", "yolo")
	fireLaunchEvents(store, "ephemeral-safe", "/tmp/x", "safe")
}

// --- printBanner -------------------------------------------------------------

// printBanner is a thin wrapper over bannerFor + Printer.Magenta/Success.
// We test it via a buffered Printer to assert both styled paths are taken
// without color-stripping the output.
func TestPrintBanner(t *testing.T) {
	t.Run("yolo path produces magenta banner with text", func(t *testing.T) {
		buf := &bufWriter{}
		out := output.NewPrinter(buf)
		printBanner(out, "yolo")
		if !strings.Contains(buf.s, "YOLO MODE") {
			t.Errorf("yolo banner missing YOLO MODE: %q", buf.s)
		}
	})
	t.Run("safe path produces success banner with text", func(t *testing.T) {
		buf := &bufWriter{}
		out := output.NewPrinter(buf)
		printBanner(out, "safe")
		if !strings.Contains(buf.s, "SAFE MODE") {
			t.Errorf("safe banner missing SAFE MODE: %q", buf.s)
		}
	})
}

// --- resolveModeTarget invalid name ------------------------------------------

func TestResolveModeTargetRejectsInvalidName(t *testing.T) {
	tmp := t.TempDir()
	tc := tmux.NewClient("")
	store := session.NewStore(filepath.Join(tmp, "sessions.json"))

	// Names containing '/' are rejected by session.ValidateName.
	_, _, err := resolveModeTarget([]string{"bad/name"}, store, tc)
	if err == nil {
		t.Fatal("expected validation error for 'bad/name', got nil")
	}
}

// --- helpers -----------------------------------------------------------------

type bufWriter struct{ s string }

func (b *bufWriter) Write(p []byte) (int, error) {
	b.s += string(p)
	return len(p), nil
}

// --- io_discard helper -------------------------------------------------------

// io_discard is a minimal io.Writer that swallows all writes. We don't
// import io/ioutil to keep it explicit and avoid the deprecated alias.
type io_discard struct{}

func (io_discard) Write(p []byte) (int, error) { return len(p), nil }
