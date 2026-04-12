package config

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Config holds user preferences for ctm.
type Config struct {
	RequiredEnv             []string `json:"required_env"`
	RequiredInPath          []string `json:"required_in_path"`
	ScrollbackLines         int      `json:"scrollback_lines"`
	HealthCheckTimeoutSec   int      `json:"health_check_timeout_seconds"`
	GitCheckpointBeforeYolo bool     `json:"git_checkpoint_before_yolo"`
	DefaultMode             string   `json:"default_mode"`
}

// Default returns a Config populated with sensible defaults.
func Default() Config {
	return Config{
		RequiredEnv:             []string{"PATH", "HOME"},
		RequiredInPath:          []string{"claude", "node", "go", "bun"},
		ScrollbackLines:         50000,
		HealthCheckTimeoutSec:   5,
		GitCheckpointBeforeYolo: true,
		DefaultMode:             "safe",
	}
}

// Dir returns the ctm config directory (~/.config/ctm/).
func Dir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".config", "ctm")
	}
	return filepath.Join(home, ".config", "ctm")
}

// ConfigPath returns the path to the main config file.
func ConfigPath() string {
	return filepath.Join(Dir(), "config.json")
}

// SessionsPath returns the path to the sessions file.
func SessionsPath() string {
	return filepath.Join(Dir(), "sessions.json")
}

// TmuxConfPath returns the path to the tmux config file.
func TmuxConfPath() string {
	return filepath.Join(Dir(), "tmux.conf")
}

// ClaudeOverlayPath returns the path to the optional claude settings overlay.
// When this file exists, ctm passes --settings <path> to every claude
// invocation, layering it on top of the user's existing claude settings
// without modifying ~/.claude/settings.json.
func ClaudeOverlayPath() string {
	return filepath.Join(Dir(), "claude-overlay.json")
}

// EnvFilePath returns the path to the optional ctm-managed env file.
// When this file exists, ctm sources it in the shell before spawning claude.
// Use this for env vars that must exist as real shell environment (e.g.
// CLAUDE_CODE_NO_FLICKER) rather than inside claude's settings.json env key,
// which is evaluated too late in claude's startup.
func EnvFilePath() string {
	return filepath.Join(Dir(), "env.sh")
}

// Load reads Config from path. If the file does not exist it creates it with
// defaults and returns those defaults. Any other error is returned to the caller.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		cfg := Default()
		if writeErr := write(path, cfg); writeErr != nil {
			return cfg, writeErr
		}
		return cfg, nil
	}
	if err != nil {
		return Config{}, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// write marshals cfg to path, creating parent directories as needed.
func write(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
