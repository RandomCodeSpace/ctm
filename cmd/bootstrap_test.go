package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/config"
)

// withTempHome redirects HOME to a fresh temp dir for the duration of t.
// config.Dir() reads HOME, so all ctm paths get rerouted underneath.
func withTempHome(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	t.Setenv("HOME", tmp)
	return tmp
}

func TestEnsureSetupCreatesAllArtifacts(t *testing.T) {
	home := withTempHome(t)

	cfg, err := ensureSetup()
	if err != nil {
		t.Fatalf("ensureSetup: %v", err)
	}
	if cfg == nil {
		t.Fatal("ensureSetup returned nil config")
	}
	if cfg.ScrollbackLines <= 0 {
		t.Errorf("expected default scrollback lines, got %d", cfg.ScrollbackLines)
	}

	cfgDir := filepath.Join(home, ".config", "ctm")
	wantFiles := []string{
		config.ConfigPath(),
		config.TmuxConfPath(),
		config.ClaudeOverlayPath(),
		config.ClaudeEnvPath(),
	}
	for _, p := range wantFiles {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}

	logs := filepath.Join(cfgDir, "logs")
	if st, err := os.Stat(logs); err != nil || !st.IsDir() {
		t.Errorf("expected %s to be a directory", logs)
	}
}

func TestEnsureSetupIdempotent(t *testing.T) {
	withTempHome(t)
	if _, err := ensureSetup(); err != nil {
		t.Fatalf("first call: %v", err)
	}

	overlayPath := config.ClaudeOverlayPath()
	marker := []byte("// user edit — must survive bootstrap\n")
	orig, err := os.ReadFile(overlayPath)
	if err != nil {
		t.Fatal(err)
	}
	edited := append(orig, marker...)
	if err := os.WriteFile(overlayPath, edited, 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := ensureSetup(); err != nil {
		t.Fatalf("second call: %v", err)
	}
	after, err := os.ReadFile(overlayPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(edited) {
		t.Errorf("ensureSetup clobbered overlay edits\nbefore: %q\nafter:  %q", edited, after)
	}
}

func TestEnsureSetupAliasesIdempotent(t *testing.T) {
	home := withTempHome(t)
	bashrc := filepath.Join(home, ".bashrc")
	// Seed a bashrc — ensureAliases only writes to existing rc files.
	if err := os.WriteFile(bashrc, []byte("# existing rc\n"), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := ensureSetup(); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(bashrc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ensureSetup(); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(bashrc)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Errorf("bashrc changed on second bootstrap:\nfirst:  %q\nsecond: %q", first, second)
	}
	if !containsAliasMarker(string(first)) {
		t.Errorf("expected alias marker injected, got:\n%s", first)
	}
}

func TestEnsureOverlayCreatesWithHookCommands(t *testing.T) {
	withTempHome(t)
	if err := ensureOverlaySidecars(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(config.ClaudeOverlayPath())
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	for _, want := range []string{`"spinnerTipsEnabled": false`, "statusline", "log-tool-use"} {
		if !contains(got, want) {
			t.Errorf("overlay missing %q:\n%s", want, got)
		}
	}
}

func TestOverlayAndEnvFilePermsAre0600(t *testing.T) {
	withTempHome(t)
	if err := ensureOverlaySidecars(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		config.ClaudeOverlayPath(),
		config.ClaudeEnvPath(),
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if mode := info.Mode().Perm(); mode != 0600 {
			t.Errorf("%s mode = %v, want 0600", path, mode)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func containsAliasMarker(s string) bool {
	return indexOf(s, "# --- ctm aliases START ---") >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
