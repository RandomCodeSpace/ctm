package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/jsonstrict"
	"github.com/RandomCodeSpace/ctm/internal/logrotate"
	"github.com/RandomCodeSpace/ctm/internal/migrate"
)

// SchemaVersion is the current on-disk schema version of config.json.
// Bump this and append a Step to the Plan returned by MigrationPlan()
// whenever the shape of Config changes in a non-additive way.
//
// v2: rewrite legacy required_in_path entries of "claude" to "codex"
// after the claude CLI was removed. Existing configs that customized
// the list to add extra binaries keep those additions; only the exact
// literal "claude" is swapped.
const SchemaVersion = 2

// Config holds user preferences for ctm.
type Config struct {
	// SchemaVersion is stamped onto config.json by the migrate runner on
	// startup. Exposed so Load/write round-trip preserves it; callers
	// should not set it explicitly — Default() and write() handle it.
	SchemaVersion int `json:"schema_version"`

	RequiredEnv             []string `json:"required_env"`
	RequiredInPath          []string `json:"required_in_path"`
	ScrollbackLines         int      `json:"scrollback_lines"`
	HealthCheckTimeoutSec   int      `json:"health_check_timeout_seconds"`
	GitCheckpointBeforeYolo bool     `json:"git_checkpoint_before_yolo"`
	DefaultMode             string   `json:"default_mode"`

	// Log rotation knobs for ~/.config/ctm/logs/<session>.jsonl.
	// A zero value means "use the built-in default" (50 MiB / 30 d / 10
	// files). To effectively disable a cap, set it to a very large number
	// rather than 0. LogPolicy() resolves zeros to defaults.
	LogMaxSizeMB  int `json:"log_max_size_mb"`
	LogMaxAgeDays int `json:"log_max_age_days"`
	LogMaxFiles   int `json:"log_max_files"`

	// Hooks maps lifecycle event names (on_attach, on_new, on_yolo,
	// on_safe, on_kill) to shell commands run when the event fires.
	// Nil / empty means no hooks. Commands run with CTM_EVENT +
	// CTM_SESSION_{NAME,UUID,MODE,WORKDIR} in the env and are bounded
	// by HookTimeoutSec seconds. See internal/hooks for the full
	// contract.
	Hooks map[string]string `json:"hooks"`

	// HookTimeoutSec is the per-hook wall-clock ceiling. Zero → default
	// (5 s). Set a very large number to effectively disable the cap.
	HookTimeoutSec int `json:"hook_timeout_seconds"`
}

// Default values for log rotation knobs. Exposed as constants so callers
// can reference them in tests and docs without hardcoding.
const (
	DefaultLogMaxSizeMB  = 50
	DefaultLogMaxAgeDays = 30
	DefaultLogMaxFiles   = 10
)

// DefaultHookTimeoutSec mirrors internal/hooks.DefaultTimeout. Kept in
// sync manually so this package doesn't depend on internal/hooks (which
// would create a cycle via cmd).
const DefaultHookTimeoutSec = 5

// HookTimeout returns the per-hook wall-clock ceiling. A zero
// HookTimeoutSec resolves to DefaultHookTimeoutSec so old configs (and
// users who never set it) still get a sensible default.
func (c Config) HookTimeout() time.Duration {
	if c.HookTimeoutSec <= 0 {
		return DefaultHookTimeoutSec * time.Second
	}
	return time.Duration(c.HookTimeoutSec) * time.Second
}

// Default returns a Config populated with sensible defaults.
func Default() Config {
	return Config{
		SchemaVersion:           SchemaVersion,
		RequiredEnv:             []string{"PATH", "HOME"},
		RequiredInPath:          []string{"codex", "node", "go", "bun"},
		ScrollbackLines:         50000,
		HealthCheckTimeoutSec:   5,
		GitCheckpointBeforeYolo: true,
		DefaultMode:             "safe",
		LogMaxSizeMB:            DefaultLogMaxSizeMB,
		LogMaxAgeDays:           DefaultLogMaxAgeDays,
		LogMaxFiles:             DefaultLogMaxFiles,
	}
}

// LogPolicy builds the logrotate.Policy from this Config, substituting
// the built-in defaults for any knob left at 0. Config files written
// before the log-rotation knobs existed load those fields as 0, so this
// is how old installs get sensible retention without a schema bump.
func (c Config) LogPolicy() logrotate.Policy {
	sizeMB := c.LogMaxSizeMB
	if sizeMB <= 0 {
		sizeMB = DefaultLogMaxSizeMB
	}
	ageDays := c.LogMaxAgeDays
	if ageDays <= 0 {
		ageDays = DefaultLogMaxAgeDays
	}
	files := c.LogMaxFiles
	if files <= 0 {
		files = DefaultLogMaxFiles
	}
	return logrotate.Policy{
		MaxSize:  int64(sizeMB) << 20,
		MaxAge:   time.Duration(ageDays) * 24 * time.Hour,
		MaxFiles: files,
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

// Load reads Config from path. If the file does not exist it creates it
// with defaults and returns those defaults.
//
// The decoder is strict: unknown top-level keys are rejected. On the
// first load that encounters an unknown key (typo, dropped experimental
// field, etc.), ctm copies the original bytes to a sibling
// ".bak.unknowns.<unix-nano>", strips the unknown keys, rewrites the
// file, and emits a WARN-level slog line naming each dropped key. See
// internal/jsonstrict for the full contract. This self-heals once, then
// strictness catches future typos immediately.
func Load(path string) (Config, error) {
	var cfg Config
	err := jsonstrict.Decode(path, &cfg)
	if os.IsNotExist(err) {
		cfg = Default()
		if writeErr := write(path, cfg); writeErr != nil {
			return cfg, writeErr
		}
		return cfg, nil
	}
	if err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// MigrationPlan returns the migrate.Plan for config.json.
//
//   - v0 → v1: stamp only (initial schema_version introduction).
//   - v1 → v2: rewrite literal "claude" entries in required_in_path
//     to "codex" after the claude CLI was removed.
//
// Callers run the returned Plan before Load so the typed unmarshal sees
// a file already at the current SchemaVersion.
func MigrationPlan() migrate.Plan {
	return migrate.Plan{
		Name:           "config.json",
		CurrentVersion: SchemaVersion,
		Steps: []migrate.Step{
			nil,                       // v0 → v1: stamp only
			rewriteRequiredPathClaude, // v1 → v2: claude → codex in required_in_path
		},
	}
}

// rewriteRequiredPathClaude rewrites the literal string "claude" in
// obj["required_in_path"] to "codex". Idempotent — a slice that
// already contains "codex" instead of "claude" passes through
// unchanged. Non-string entries (defensive) are passed through verbatim.
//
// Custom additions ("claude", "node", "go", "bun", "rust") are
// preserved aside from the targeted swap, so users who tailored the
// list keep their tailoring.
func rewriteRequiredPathClaude(obj map[string]json.RawMessage) error {
	raw, ok := obj["required_in_path"]
	if !ok || len(raw) == 0 {
		return nil
	}
	var list []json.RawMessage
	if err := json.Unmarshal(raw, &list); err != nil {
		// Malformed entry — leave it alone; jsonstrict will surface
		// the error on the typed Load that follows.
		return nil
	}
	changed := false
	for i, entry := range list {
		var s string
		if err := json.Unmarshal(entry, &s); err != nil {
			continue
		}
		if s == "claude" {
			list[i] = json.RawMessage(`"codex"`)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	out, err := json.Marshal(list)
	if err != nil {
		return err
	}
	obj["required_in_path"] = out
	return nil
}

// write marshals cfg to path, creating parent directories as needed. It
// always stamps the current SchemaVersion so the file round-trips cleanly
// through the migrator on subsequent loads.
func write(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	cfg.SchemaVersion = SchemaVersion
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	// 0600: config.json is personal preference state under ~/.config/ctm/
	// (a 0700 dir); no need for it to be readable by other users on shared
	// hosts.
	return os.WriteFile(path, data, 0600)
}
