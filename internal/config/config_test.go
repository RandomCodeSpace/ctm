package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultConfig(t *testing.T) {
	cfg := Default()

	if len(cfg.RequiredEnv) != 2 || cfg.RequiredEnv[0] != "PATH" || cfg.RequiredEnv[1] != "HOME" {
		t.Errorf("RequiredEnv = %v, want [PATH HOME]", cfg.RequiredEnv)
	}

	expectedInPath := []string{"claude", "node", "go", "bun"}
	if len(cfg.RequiredInPath) != len(expectedInPath) {
		t.Errorf("RequiredInPath len = %d, want %d", len(cfg.RequiredInPath), len(expectedInPath))
	} else {
		for i, v := range expectedInPath {
			if cfg.RequiredInPath[i] != v {
				t.Errorf("RequiredInPath[%d] = %q, want %q", i, cfg.RequiredInPath[i], v)
			}
		}
	}

	if cfg.ScrollbackLines != 50000 {
		t.Errorf("ScrollbackLines = %d, want 50000", cfg.ScrollbackLines)
	}

	if cfg.HealthCheckTimeoutSec != 5 {
		t.Errorf("HealthCheckTimeoutSec = %d, want 5", cfg.HealthCheckTimeoutSec)
	}

	if !cfg.GitCheckpointBeforeYolo {
		t.Errorf("GitCheckpointBeforeYolo = false, want true")
	}

	if cfg.DefaultMode != "safe" {
		t.Errorf("DefaultMode = %q, want \"safe\"", cfg.DefaultMode)
	}
}

func TestLoadCreatesDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	// Should have defaults
	if cfg.ScrollbackLines != 50000 {
		t.Errorf("ScrollbackLines = %d, want 50000", cfg.ScrollbackLines)
	}

	// File should have been created
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("Load did not create config file at %s", path)
	}

	// File should contain valid JSON
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile error: %v", err)
	}
	var check Config
	if err := json.Unmarshal(data, &check); err != nil {
		t.Errorf("created file is not valid JSON: %v", err)
	}
}

func TestLoadExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	custom := Config{
		RequiredEnv:             []string{"MYVAR"},
		RequiredInPath:          []string{"mytool"},
		ScrollbackLines:         9999,
		HealthCheckTimeoutSec:   30,
		GitCheckpointBeforeYolo: false,
		DefaultMode:             "yolo",
	}
	data, err := json.MarshalIndent(custom, "", "  ")
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("WriteFile error: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load error: %v", err)
	}

	if cfg.ScrollbackLines != 9999 {
		t.Errorf("ScrollbackLines = %d, want 9999", cfg.ScrollbackLines)
	}
	if cfg.DefaultMode != "yolo" {
		t.Errorf("DefaultMode = %q, want \"yolo\"", cfg.DefaultMode)
	}
	if cfg.GitCheckpointBeforeYolo {
		t.Errorf("GitCheckpointBeforeYolo = true, want false")
	}
	if len(cfg.RequiredEnv) != 1 || cfg.RequiredEnv[0] != "MYVAR" {
		t.Errorf("RequiredEnv = %v, want [MYVAR]", cfg.RequiredEnv)
	}
}

func TestConfigDir(t *testing.T) {
	d := Dir()
	if d == "" {
		t.Error("Dir() returned empty string")
	}
}
