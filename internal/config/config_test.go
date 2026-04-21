package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestDefaultStampsSchemaVersion(t *testing.T) {
	if got := Default().SchemaVersion; got != SchemaVersion {
		t.Errorf("Default().SchemaVersion = %d, want %d", got, SchemaVersion)
	}
}

func TestLoadCreatesFileWithSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	if _, err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if v := string(raw["schema_version"]); v != "1" {
		t.Errorf("freshly-created config.json schema_version = %s, want 1", v)
	}
}

func TestWriteForceStampsSchemaVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// Caller deliberately passes a zero SchemaVersion; write must fix it
	// up so the file always round-trips through the migrator.
	cfg := Default()
	cfg.SchemaVersion = 0

	if err := write(path, cfg); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, _ := os.ReadFile(path)
	var raw map[string]json.RawMessage
	_ = json.Unmarshal(data, &raw)
	if v := string(raw["schema_version"]); v != "1" {
		t.Errorf("write stamped schema_version = %s, want 1", v)
	}
}

func TestWritePermIs0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := write(path, Default()); err != nil {
		t.Fatalf("write: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("config.json mode = %v, want 0600", mode)
	}
}

func TestMigrationPlan_MatchesSchemaVersion(t *testing.T) {
	p := MigrationPlan()
	if p.CurrentVersion != SchemaVersion {
		t.Errorf("MigrationPlan.CurrentVersion = %d, want %d", p.CurrentVersion, SchemaVersion)
	}
	if len(p.Steps) != SchemaVersion {
		t.Errorf("MigrationPlan has %d steps, want %d (must equal CurrentVersion)", len(p.Steps), SchemaVersion)
	}
}

func TestLogPolicy_ZerosResolveToDefaults(t *testing.T) {
	cfg := Config{} // all zero-valued
	p := cfg.LogPolicy()

	wantSize := int64(DefaultLogMaxSizeMB) << 20
	if p.MaxSize != wantSize {
		t.Errorf("MaxSize = %d, want %d", p.MaxSize, wantSize)
	}
	if p.MaxAge != time.Duration(DefaultLogMaxAgeDays)*24*time.Hour {
		t.Errorf("MaxAge = %v, want %d days", p.MaxAge, DefaultLogMaxAgeDays)
	}
	if p.MaxFiles != DefaultLogMaxFiles {
		t.Errorf("MaxFiles = %d, want %d", p.MaxFiles, DefaultLogMaxFiles)
	}
}

func TestLogPolicy_ExplicitValuesRespected(t *testing.T) {
	cfg := Config{LogMaxSizeMB: 200, LogMaxAgeDays: 7, LogMaxFiles: 3}
	p := cfg.LogPolicy()

	if p.MaxSize != 200<<20 {
		t.Errorf("MaxSize = %d, want %d", p.MaxSize, 200<<20)
	}
	if p.MaxAge != 7*24*time.Hour {
		t.Errorf("MaxAge = %v, want 7d", p.MaxAge)
	}
	if p.MaxFiles != 3 {
		t.Errorf("MaxFiles = %d, want 3", p.MaxFiles)
	}
}

func TestDefault_PopulatesServeDefaults(t *testing.T) {
	cfg := Default()
	if cfg.Serve.Port != DefaultServePort {
		t.Errorf("Serve.Port = %d, want %d", cfg.Serve.Port, DefaultServePort)
	}
	want := AttentionThresholds{
		ErrorRatePct:         DefaultAttentionErrorRatePct,
		ErrorRateWindow:      DefaultAttentionErrorRateWindow,
		IdleMinutes:          DefaultAttentionIdleMinutes,
		QuotaPct:             DefaultAttentionQuotaPct,
		ContextPct:           DefaultAttentionContextPct,
		YoloUncheckedMinutes: DefaultAttentionYoloUncheckedMinutes,
	}
	if cfg.Serve.Attention != want {
		t.Errorf("Serve.Attention = %+v, want %+v", cfg.Serve.Attention, want)
	}
	// Optional/user-supplied fields must stay empty by default.
	if cfg.Serve.BearerToken != "" || cfg.Serve.WebhookURL != "" ||
		cfg.Serve.WebhookAuth != "" || cfg.Serve.StatuslineDumpDir != "" {
		t.Errorf("optional serve fields populated unexpectedly: %+v", cfg.Serve)
	}
}

func TestAttentionThresholds_ResolvedFillsZeros(t *testing.T) {
	got := AttentionThresholds{}.Resolved()
	want := AttentionThresholds{
		ErrorRatePct:         DefaultAttentionErrorRatePct,
		ErrorRateWindow:      DefaultAttentionErrorRateWindow,
		IdleMinutes:          DefaultAttentionIdleMinutes,
		QuotaPct:             DefaultAttentionQuotaPct,
		ContextPct:           DefaultAttentionContextPct,
		YoloUncheckedMinutes: DefaultAttentionYoloUncheckedMinutes,
	}
	if got != want {
		t.Errorf("Resolved() on zero = %+v, want %+v", got, want)
	}
}

func TestAttentionThresholds_ResolvedPreservesExplicit(t *testing.T) {
	in := AttentionThresholds{
		ErrorRatePct:         1,
		ErrorRateWindow:      2,
		IdleMinutes:          3,
		QuotaPct:             4,
		ContextPct:           5,
		YoloUncheckedMinutes: 6,
	}
	if got := in.Resolved(); got != in {
		t.Errorf("Resolved() altered explicit values: %+v -> %+v", in, got)
	}
}

func TestServeConfig_ResolvedPortFallsBackToDefault(t *testing.T) {
	if got := (ServeConfig{}).ResolvedPort(); got != DefaultServePort {
		t.Errorf("ResolvedPort() zero = %d, want %d", got, DefaultServePort)
	}
	if got := (ServeConfig{Port: 9999}).ResolvedPort(); got != 9999 {
		t.Errorf("ResolvedPort() explicit = %d, want 9999", got)
	}
}

func TestLoad_ConfigWithoutServeKeyStillParses(t *testing.T) {
	// Existing v0.1 config.json files predate the serve block. Strict
	// decoding tolerates missing subkeys (only unknown ones fail), so
	// these must load clean with zero-valued Serve + no backup written.
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	legacy := `{
  "schema_version": 1,
  "scrollback_lines": 4242,
  "default_mode": "safe"
}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load on pre-serve config: %v", err)
	}
	if cfg.ScrollbackLines != 4242 {
		t.Errorf("ScrollbackLines = %d, want 4242", cfg.ScrollbackLines)
	}
	if cfg.Serve != (ServeConfig{}) {
		t.Errorf("Serve should be zero-valued on legacy load, got %+v", cfg.Serve)
	}
	// Accessors must still return sensible defaults on a zero Serve.
	if got := cfg.Serve.ResolvedPort(); got != DefaultServePort {
		t.Errorf("ResolvedPort() on legacy = %d, want %d", got, DefaultServePort)
	}

	// No unknown-keys backup should have been produced — the key was
	// absent, not unknown.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".bak.unknowns.") {
			t.Errorf("unexpected unknowns backup created: %s", e.Name())
		}
	}
}

func TestLoad_ServeBlockRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	orig := Default()
	orig.Serve = ServeConfig{
		Port:              40000,
		BearerToken:       "tok-abc",
		WebhookURL:        "https://example.invalid/hook",
		WebhookAuth:       "Bearer xyz",
		StatuslineDumpDir: "/var/tmp/ctm-sl",
		Attention: AttentionThresholds{
			ErrorRatePct:         33,
			ErrorRateWindow:      50,
			IdleMinutes:          7,
			QuotaPct:             90,
			ContextPct:           95,
			YoloUncheckedMinutes: 15,
		},
	}

	if err := write(path, orig); err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.Serve != orig.Serve {
		t.Errorf("Serve round-trip mismatch:\n got  = %+v\n want = %+v", loaded.Serve, orig.Serve)
	}
}

func TestLoad_StripsUnknownKeysAndBacksUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	// A config with every real field PLUS a made-up one ("typo").
	original := `{
  "schema_version": 1,
  "scrollback_lines": 12345,
  "default_mode": "yolo",
  "typo": "this is not a real key"
}`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load expected to succeed after strip: %v", err)
	}
	if cfg.ScrollbackLines != 12345 || cfg.DefaultMode != "yolo" {
		t.Errorf("typed fields dropped: %+v", cfg)
	}

	// Confirm an unknowns-backup sibling exists with the original bytes.
	entries, _ := os.ReadDir(dir)
	var backup string
	for _, e := range entries {
		if strings.Contains(e.Name(), ".bak.unknowns.") {
			backup = filepath.Join(dir, e.Name())
			break
		}
	}
	if backup == "" {
		t.Fatal("expected a .bak.unknowns.* backup, found none")
	}
	bdata, _ := os.ReadFile(backup)
	if string(bdata) != original {
		t.Errorf("backup diverges from original")
	}

	// Rewritten file must no longer contain the stripped key.
	after, _ := os.ReadFile(path)
	if strings.Contains(string(after), `"typo"`) {
		t.Errorf("stripped key survived rewrite: %s", after)
	}
}
