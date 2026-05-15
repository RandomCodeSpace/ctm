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

func TestEnsureSetupCreatesConfigAndTmuxConf(t *testing.T) {
	withTempHome(t)

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

	for _, p := range []string{config.ConfigPath(), config.TmuxConfPath()} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected %s to exist: %v", p, err)
		}
	}
}

func TestEnsureSetupIsIdempotent(t *testing.T) {
	withTempHome(t)

	if _, err := ensureSetup(); err != nil {
		t.Fatalf("first ensureSetup: %v", err)
	}

	cfgPath := config.ConfigPath()
	stat1, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ensureSetup(); err != nil {
		t.Fatalf("second ensureSetup: %v", err)
	}

	stat2, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	if stat1.Size() != stat2.Size() {
		t.Errorf("config.json size changed across idempotent calls (%d → %d)", stat1.Size(), stat2.Size())
	}
}

func TestEnsureSetupWritesDefaultConfigContents(t *testing.T) {
	withTempHome(t)

	if _, err := ensureSetup(); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(config.ConfigPath())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	hasCodex := false
	for _, p := range cfg.RequiredInPath {
		if p == "codex" {
			hasCodex = true
		}
	}
	if !hasCodex {
		t.Errorf("expected default RequiredInPath to include \"codex\", got %v", cfg.RequiredInPath)
	}

	// Verify no stray overlay/env files were created — those features
	// were removed when claude support was dropped.
	cfgDir := filepath.Dir(config.ConfigPath())
	for _, leaked := range []string{
		filepath.Join(cfgDir, "claude-overlay.json"),
		filepath.Join(cfgDir, "claude-env.json"),
	} {
		if _, err := os.Stat(leaked); err == nil {
			t.Errorf("unexpected legacy file present: %s", leaked)
		}
	}
}
