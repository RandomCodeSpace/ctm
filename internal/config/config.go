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
const SchemaVersion = 1

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

	// Serve holds configuration for `ctm serve` (the local UI daemon).
	// Missing from old configs is fine: strict decoding tolerates absent
	// keys, and zero-valued fields fall back to built-in defaults via
	// their accessor helpers.
	Serve ServeConfig `json:"serve"`
}

// ServeConfig holds knobs for the `ctm serve` daemon. All fields are
// optional; zero values resolve to defaults via ServeConfig accessors.
type ServeConfig struct {
	Port              int                 `json:"port"`
	BearerToken       string              `json:"bearer_token"`
	WebhookURL        string              `json:"webhook_url"`
	WebhookAuth       string              `json:"webhook_auth"`
	StatuslineDumpDir string              `json:"statusline_dump_dir"`
	Attention         AttentionThresholds `json:"attention"`
}

// AttentionThresholds controls when `ctm serve` flags a session as
// needing user attention. Zero fields resolve to defaults via
// Resolved().
type AttentionThresholds struct {
	ErrorRatePct         int `json:"error_rate_pct"`
	ErrorRateWindow      int `json:"error_rate_window"`
	IdleMinutes          int `json:"idle_minutes"`
	QuotaPct             int `json:"quota_pct"`
	ContextPct           int `json:"context_pct"`
	YoloUncheckedMinutes int `json:"yolo_unchecked_minutes"`
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

// Defaults for `ctm serve` attention thresholds and port. Exposed so
// callers can reference them without re-hardcoding the numeric literal.
const (
	DefaultServePort                     = 37778
	DefaultAttentionErrorRatePct         = 20
	DefaultAttentionErrorRateWindow      = 20
	DefaultAttentionIdleMinutes          = 5
	DefaultAttentionQuotaPct             = 85
	DefaultAttentionContextPct           = 90
	DefaultAttentionYoloUncheckedMinutes = 30
)

// Port returns the listen port for `ctm serve`, substituting
// DefaultServePort when the configured value is non-positive so old
// configs that predate the serve block still bind the canonical port.
func (s ServeConfig) ResolvedPort() int {
	if s.Port <= 0 {
		return DefaultServePort
	}
	return s.Port
}

// Resolved returns a copy of t with any zero-valued field replaced by
// its built-in default. Mirrors LogPolicy()/HookTimeout(): old configs
// loaded without the attention block get sane thresholds without a
// schema bump.
func (t AttentionThresholds) Resolved() AttentionThresholds {
	if t.ErrorRatePct <= 0 {
		t.ErrorRatePct = DefaultAttentionErrorRatePct
	}
	if t.ErrorRateWindow <= 0 {
		t.ErrorRateWindow = DefaultAttentionErrorRateWindow
	}
	if t.IdleMinutes <= 0 {
		t.IdleMinutes = DefaultAttentionIdleMinutes
	}
	if t.QuotaPct <= 0 {
		t.QuotaPct = DefaultAttentionQuotaPct
	}
	if t.ContextPct <= 0 {
		t.ContextPct = DefaultAttentionContextPct
	}
	if t.YoloUncheckedMinutes <= 0 {
		t.YoloUncheckedMinutes = DefaultAttentionYoloUncheckedMinutes
	}
	return t
}

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
		RequiredInPath:          []string{"claude", "node", "go", "bun"},
		ScrollbackLines:         50000,
		HealthCheckTimeoutSec:   5,
		GitCheckpointBeforeYolo: true,
		DefaultMode:             "safe",
		LogMaxSizeMB:            DefaultLogMaxSizeMB,
		LogMaxAgeDays:           DefaultLogMaxAgeDays,
		LogMaxFiles:             DefaultLogMaxFiles,
		Serve: ServeConfig{
			Port: DefaultServePort,
			Attention: AttentionThresholds{
				ErrorRatePct:         DefaultAttentionErrorRatePct,
				ErrorRateWindow:      DefaultAttentionErrorRateWindow,
				IdleMinutes:          DefaultAttentionIdleMinutes,
				QuotaPct:             DefaultAttentionQuotaPct,
				ContextPct:           DefaultAttentionContextPct,
				YoloUncheckedMinutes: DefaultAttentionYoloUncheckedMinutes,
			},
		},
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

// MigrationPlan returns the migrate.Plan for config.json. Steps is empty at
// v1 because the initial migration only stamps the version — no content
// changes are required to turn an unversioned config.json into v1.
//
// Callers run the returned Plan before Load so the typed unmarshal sees
// a file already at the current SchemaVersion.
func MigrationPlan() migrate.Plan {
	return migrate.Plan{
		Name:           "config.json",
		CurrentVersion: SchemaVersion,
		Steps:          []migrate.Step{nil}, // v0 → v1: stamp only
	}
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
