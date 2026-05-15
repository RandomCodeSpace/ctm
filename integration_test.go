//go:build integration

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ctmBin(t *testing.T) string {
	t.Helper()
	return "./ctm"
}

func ctmRun(t *testing.T, home string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("./ctm", args...)
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestIntegration_Version(t *testing.T) {
	home := t.TempDir()
	out, err := ctmRun(t, home, "version")
	if err != nil {
		t.Fatalf("ctm version failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "ctm") {
		t.Errorf("expected output to contain 'ctm', got: %s", out)
	}
}

func TestIntegration_Help(t *testing.T) {
	home := t.TempDir()
	out, err := ctmRun(t, home, "help")
	if err != nil {
		t.Fatalf("ctm help failed: %v\noutput: %s", err, out)
	}
	for _, cmd := range []string{"ls", "new", "kill", "yolo", "safe", "check", "doctor"} {
		if !strings.Contains(out, cmd) {
			t.Errorf("expected help output to contain %q, got:\n%s", cmd, out)
		}
	}
}

func TestIntegration_ListEmpty(t *testing.T) {
	home := t.TempDir()
	out, err := ctmRun(t, home, "ls")
	if err != nil {
		t.Fatalf("ctm ls failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(strings.ToLower(out), "no") {
		t.Errorf("expected output to contain 'no' (case insensitive) for empty list, got: %s", out)
	}
}

func TestIntegration_Doctor(t *testing.T) {
	home := t.TempDir()
	out, err := ctmRun(t, home, "doctor")
	// doctor may exit non-zero if tools are missing, but should still produce output
	_ = err
	if !strings.Contains(strings.ToLower(out), "tmux") {
		t.Errorf("expected doctor output to mention 'tmux', got: %s", out)
	}
}

func TestIntegration_CreateAndKill(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("skipping tmux session test in CI")
	}

	// Check tmux is available
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux not available")
	}

	home := t.TempDir()
	sessionName := fmt.Sprintf("ctm-test-%d", time.Now().UnixNano())

	// Create session — ctm new creates the tmux session and saves state, then
	// tries to attach (tc.Go). In a non-terminal test environment the attach
	// step fails, but the session and state file are already written. We
	// therefore tolerate a non-zero exit here and check the side-effects.
	out, err := ctmRun(t, home, "new", sessionName, "/tmp")
	// If there is an error it should only be the terminal-attach step.
	if err != nil {
		if !strings.Contains(out, "created session") {
			t.Fatalf("ctm new failed before creating session: %v\noutput: %s", err, out)
		}
		// Session was created; attach failed as expected in a non-tty env — continue.
	}

	// Cleanup tmux session on test exit
	t.Cleanup(func() {
		exec.Command("tmux", "kill-session", "-t", sessionName).Run() //nolint:errcheck
	})

	// Verify tmux session exists
	if err := exec.Command("tmux", "has-session", "-t", sessionName).Run(); err != nil {
		t.Errorf("expected tmux session %q to exist after ctm new, but tmux has-session failed: %v", sessionName, err)
	}

	// Verify state file has session
	sessionsPath := filepath.Join(home, ".config", "ctm", "sessions.json")
	data, err := os.ReadFile(sessionsPath)
	if err != nil {
		t.Fatalf("could not read sessions file %s: %v", sessionsPath, err)
	}
	if !strings.Contains(string(data), sessionName) {
		t.Errorf("expected sessions.json to contain %q, got: %s", sessionName, string(data))
	}

	// Kill the session
	out, err = ctmRun(t, home, "kill", sessionName)
	if err != nil {
		t.Fatalf("ctm kill failed: %v\noutput: %s", err, out)
	}

	// Verify tmux session is gone
	if err := exec.Command("tmux", "has-session", "-t", sessionName).Run(); err == nil {
		t.Errorf("expected tmux session %q to be gone after ctm kill, but it still exists", sessionName)
	}

	// Verify state file no longer has session
	data, err = os.ReadFile(sessionsPath)
	if err != nil && !os.IsNotExist(err) {
		t.Fatalf("could not read sessions file after kill: %v", err)
	}
	if err == nil && strings.Contains(string(data), sessionName) {
		t.Errorf("expected sessions.json to NOT contain %q after kill, got: %s", sessionName, string(data))
	}
}

func TestIntegration_ForgetNonExistent(t *testing.T) {
	home := t.TempDir()
	out, err := ctmRun(t, home, "forget", "nonexistent")
	if err == nil {
		t.Errorf("expected ctm forget nonexistent to return error, but got nil\noutput: %s", out)
	}
}

func TestIntegration_KillAllEmpty(t *testing.T) {
	home := t.TempDir()
	out, err := ctmRun(t, home, "killall")
	if err != nil {
		t.Fatalf("ctm killall failed: %v\noutput: %s", err, out)
	}
	if !strings.Contains(strings.ToLower(out), "no") {
		t.Errorf("expected killall with no sessions to print 'no' message, got: %s", out)
	}
}

func TestIntegration_Install(t *testing.T) {
	home := t.TempDir()

	// Create .bashrc
	bashrc := filepath.Join(home, ".bashrc")
	if err := os.WriteFile(bashrc, []byte("# existing content\n"), 0644); err != nil {
		t.Fatalf("failed to create .bashrc: %v", err)
	}

	out, err := ctmRun(t, home, "install")
	if err != nil {
		t.Fatalf("ctm install failed: %v\noutput: %s", err, out)
	}

	// Verify aliases injected into .bashrc
	bashrcData, err := os.ReadFile(bashrc)
	if err != nil {
		t.Fatalf("could not read .bashrc: %v", err)
	}
	if !strings.Contains(string(bashrcData), "ctm aliases START") {
		t.Errorf("expected aliases injected in .bashrc, got:\n%s", string(bashrcData))
	}

	// Verify config.json created
	configPath := filepath.Join(home, ".config", "ctm", "config.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("expected config.json to exist at %s: %v", configPath, err)
	}

	// Verify tmux.conf created
	tmuxConfPath := filepath.Join(home, ".config", "ctm", "tmux.conf")
	if _, err := os.Stat(tmuxConfPath); err != nil {
		t.Errorf("expected tmux.conf to exist at %s: %v", tmuxConfPath, err)
	}
}

func TestIntegration_InstallIdempotent(t *testing.T) {
	home := t.TempDir()

	// Create .bashrc
	bashrc := filepath.Join(home, ".bashrc")
	if err := os.WriteFile(bashrc, []byte("# existing content\n"), 0644); err != nil {
		t.Fatalf("failed to create .bashrc: %v", err)
	}

	// Install twice
	for i := 0; i < 2; i++ {
		out, err := ctmRun(t, home, "install")
		if err != nil {
			t.Fatalf("ctm install (run %d) failed: %v\noutput: %s", i+1, err, out)
		}
	}

	// Verify only one alias block present
	bashrcData, err := os.ReadFile(bashrc)
	if err != nil {
		t.Fatalf("could not read .bashrc: %v", err)
	}
	content := string(bashrcData)
	startCount := strings.Count(content, "ctm aliases START")
	if startCount != 1 {
		t.Errorf("expected exactly 1 alias block after 2 installs, got %d:\n%s", startCount, content)
	}
}

func TestIntegration_Uninstall(t *testing.T) {
	home := t.TempDir()

	// Create .bashrc
	bashrc := filepath.Join(home, ".bashrc")
	if err := os.WriteFile(bashrc, []byte("# existing content\n"), 0644); err != nil {
		t.Fatalf("failed to create .bashrc: %v", err)
	}

	// Install first
	out, err := ctmRun(t, home, "install")
	if err != nil {
		t.Fatalf("ctm install failed: %v\noutput: %s", err, out)
	}

	// Then uninstall
	out, err = ctmRun(t, home, "uninstall")
	if err != nil {
		t.Fatalf("ctm uninstall failed: %v\noutput: %s", err, out)
	}

	// Verify aliases removed from .bashrc
	bashrcData, err := os.ReadFile(bashrc)
	if err != nil {
		t.Fatalf("could not read .bashrc after uninstall: %v", err)
	}
	if strings.Contains(string(bashrcData), "ctm aliases START") {
		t.Errorf("expected aliases removed from .bashrc after uninstall, got:\n%s", string(bashrcData))
	}

	// Verify config dir gone
	cfgDir := filepath.Join(home, ".config", "ctm")
	if _, err := os.Stat(cfgDir); !os.IsNotExist(err) {
		t.Errorf("expected config dir %s to be gone after uninstall, but it still exists", cfgDir)
	}
}

func TestIntegration_SessionNameValidation(t *testing.T) {
	home := t.TempDir()
	out, err := ctmRun(t, home, "new", "bad name", "/tmp")
	if err == nil {
		t.Errorf("expected ctm new with invalid name to return error, got nil\noutput: %s", out)
	}
}

