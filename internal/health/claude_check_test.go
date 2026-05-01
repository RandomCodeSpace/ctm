package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/tmux"
)

// --- CheckWorkdir -----------------------------------------------------------

func TestCheckWorkdir(t *testing.T) {
	tmpDir := t.TempDir()

	existingDir := filepath.Join(tmpDir, "exists")
	if err := os.Mkdir(existingDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	existingFile := filepath.Join(tmpDir, "afile")
	if err := os.WriteFile(existingFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("writefile: %v", err)
	}

	missing := filepath.Join(tmpDir, "does-not-exist")

	tests := []struct {
		name           string
		workdir        string
		wantStatus     string
		wantMsgSubstr  string
		wantFixSubstr  string
	}{
		{
			name:          "empty workdir fails (migrated session)",
			workdir:       "",
			wantStatus:    StatusFail,
			wantMsgSubstr: "working directory not set",
			wantFixSubstr: "ctm forget",
		},
		{
			name:          "non-existent path fails with does-not-exist",
			workdir:       missing,
			wantStatus:    StatusFail,
			wantMsgSubstr: "does not exist",
			wantFixSubstr: "mkdir -p",
		},
		{
			name:          "regular file fails with not-a-directory",
			workdir:       existingFile,
			wantStatus:    StatusFail,
			wantMsgSubstr: "not a directory",
			wantFixSubstr: "remove the file",
		},
		{
			name:          "existing directory passes",
			workdir:       existingDir,
			wantStatus:    StatusPass,
			wantMsgSubstr: "exists and is a directory",
		},
	}

	// Permission-denied path: os.Stat returns a non-IsNotExist error when
	// the parent dir is unreadable. Skipped if running as root (chmod 0
	// is bypassed for root) or if the FS doesn't enforce mode bits
	// (e.g. some CI overlays).
	if os.Geteuid() != 0 {
		lockedParent := filepath.Join(tmpDir, "locked")
		if err := os.Mkdir(lockedParent, 0o755); err != nil {
			t.Fatalf("mkdir locked: %v", err)
		}
		target := filepath.Join(lockedParent, "child")
		if err := os.Mkdir(target, 0o755); err != nil {
			t.Fatalf("mkdir target: %v", err)
		}
		if err := os.Chmod(lockedParent, 0o000); err != nil {
			t.Fatalf("chmod locked: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(lockedParent, 0o755) })

		// Verify the FS actually enforces the perm before relying on it.
		if _, err := os.Stat(target); err != nil && !os.IsNotExist(err) {
			t.Run("stat error other than not-exist fails", func(t *testing.T) {
				got := CheckWorkdir(target)
				if got.Name != "workdir" {
					t.Errorf("Name = %q, want %q", got.Name, "workdir")
				}
				if got.Status != StatusFail {
					t.Errorf("Status = %q, want %q", got.Status, StatusFail)
				}
				if !strings.Contains(got.Message, "error checking workdir") {
					t.Errorf("Message = %q, want substring %q", got.Message, "error checking workdir")
				}
			})
		}
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckWorkdir(tc.workdir)
			if got.Name != "workdir" {
				t.Errorf("Name = %q, want %q", got.Name, "workdir")
			}
			if got.Status != tc.wantStatus {
				t.Errorf("Status = %q, want %q (msg=%q)", got.Status, tc.wantStatus, got.Message)
			}
			if tc.wantMsgSubstr != "" && !strings.Contains(got.Message, tc.wantMsgSubstr) {
				t.Errorf("Message = %q, want substring %q", got.Message, tc.wantMsgSubstr)
			}
			if tc.wantFixSubstr != "" && !strings.Contains(got.Fix, tc.wantFixSubstr) {
				t.Errorf("Fix = %q, want substring %q", got.Fix, tc.wantFixSubstr)
			}
		})
	}
}

// --- CheckClaudeSession -----------------------------------------------------

// writeSessionFile creates a Claude-style session JSON file containing the uuid
// as a substring (matching the SessionExists walk pattern: *.json files).
func writeSessionFile(t *testing.T, dir, uuid string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	path := filepath.Join(dir, uuid+".json")
	body := []byte(`{"sessionId":"` + uuid + `"}`)
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestCheckClaudeSession(t *testing.T) {
	tests := []struct {
		name          string
		uuid          string
		setup         func(t *testing.T, home string)
		wantStatus    string
		wantMsgSubstr string
		wantFixSubstr string
	}{
		{
			name:          "empty uuid fails",
			uuid:          "",
			setup:         nil,
			wantStatus:    StatusFail,
			wantMsgSubstr: "session UUID is empty",
			wantFixSubstr: "valid Claude session UUID",
		},
		{
			name: "session present in projects dir passes",
			uuid: "11111111-aaaa-bbbb-cccc-222222222222",
			setup: func(t *testing.T, home string) {
				writeSessionFile(t, filepath.Join(home, ".claude", "projects", "myproj"),
					"11111111-aaaa-bbbb-cccc-222222222222")
			},
			wantStatus:    StatusPass,
			wantMsgSubstr: "exists",
		},
		{
			name: "session present in conversations dir passes",
			uuid: "33333333-dddd-eeee-ffff-444444444444",
			setup: func(t *testing.T, home string) {
				writeSessionFile(t, filepath.Join(home, ".claude", "conversations"),
					"33333333-dddd-eeee-ffff-444444444444")
			},
			wantStatus:    StatusPass,
			wantMsgSubstr: "exists",
		},
		{
			name:          "uuid not found anywhere fails",
			uuid:          "55555555-no-such-uuid-66666666",
			setup:         nil,
			wantStatus:    StatusFail,
			wantMsgSubstr: "not found",
			wantFixSubstr: "verify the session UUID",
		},
		{
			name: "claude dir exists but uuid absent fails",
			uuid: "77777777-absent-aaaaaaaa",
			setup: func(t *testing.T, home string) {
				// create empty projects + conversations dirs so the walk runs
				// but finds no matching file
				if err := os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Join(home, ".claude", "conversations"), 0o755); err != nil {
					t.Fatal(err)
				}
				// add a decoy file with a different uuid
				writeSessionFile(t, filepath.Join(home, ".claude", "projects"),
					"decoy-uuid-not-the-one-we-want")
			},
			wantStatus:    StatusFail,
			wantMsgSubstr: "not found",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			if tc.setup != nil {
				tc.setup(t, home)
			}

			got := CheckClaudeSession(tc.uuid)
			if got.Name != "claude_session" {
				t.Errorf("Name = %q, want %q", got.Name, "claude_session")
			}
			if got.Status != tc.wantStatus {
				t.Errorf("Status = %q, want %q (msg=%q)", got.Status, tc.wantStatus, got.Message)
			}
			if tc.wantMsgSubstr != "" && !strings.Contains(got.Message, tc.wantMsgSubstr) {
				t.Errorf("Message = %q, want substring %q", got.Message, tc.wantMsgSubstr)
			}
			if tc.wantFixSubstr != "" && !strings.Contains(got.Fix, tc.wantFixSubstr) {
				t.Errorf("Fix = %q, want substring %q", got.Fix, tc.wantFixSubstr)
			}
		})
	}
}

// --- CheckClaudeProcess -----------------------------------------------------

// CheckClaudeProcess shells out to tmux directly via exec.Command (the public
// PanePID method does not honour the Client's injectable execCommand hook),
// so we cannot easily stub it. We can still cover the first failure branch
// deterministically: a session name that does not exist makes
// `tmux list-panes -t <bogus>` exit non-zero, returning an error from
// PanePID. If tmux is not installed at all, exec.LookPath also fails and
// we hit the same branch. Either way the early-failure path is exercised.
//
// The remaining branches (no claude child / IsClaudeAlive error / not alive
// / alive) require a live tmux pane with a controlled child process tree
// and are out of scope for a unit test — they belong in an integration
// test with a real tmux server.
func TestCheckClaudeProcess_PanePIDError(t *testing.T) {
	tc := tmux.NewClient("")
	// A session name that almost certainly does not exist on any host.
	bogus := "ctm-health-test-bogus-session-zzz-9f8e7d6c"

	got := CheckClaudeProcess(tc, bogus)

	if got.Name != "claude_process" {
		t.Errorf("Name = %q, want %q", got.Name, "claude_process")
	}
	if got.Status != StatusFail {
		t.Errorf("Status = %q, want %q (msg=%q)", got.Status, StatusFail, got.Message)
	}
	if !strings.Contains(got.Message, "could not get pane PID") {
		t.Errorf("Message = %q, want substring %q", got.Message, "could not get pane PID")
	}
	if !strings.Contains(got.Message, bogus) {
		t.Errorf("Message = %q, want session name %q included", got.Message, bogus)
	}
	if !strings.Contains(got.Fix, "tmux session exists") {
		t.Errorf("Fix = %q, want substring %q", got.Fix, "tmux session exists")
	}
}
