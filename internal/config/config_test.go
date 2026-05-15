package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/migrate"
)

func TestDefaultConfig(t *testing.T) {
	cfg := Default()

	if len(cfg.RequiredEnv) != 2 || cfg.RequiredEnv[0] != "PATH" || cfg.RequiredEnv[1] != "HOME" {
		t.Errorf("RequiredEnv = %v, want [PATH HOME]", cfg.RequiredEnv)
	}

	expectedInPath := []string{"codex", "node", "go", "bun"}
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
	wantSV := fmt.Sprintf("%d", SchemaVersion)
	if v := string(raw["schema_version"]); v != wantSV {
		t.Errorf("freshly-created config.json schema_version = %s, want %s", v, wantSV)
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
	wantSV := fmt.Sprintf("%d", SchemaVersion)
	if v := string(raw["schema_version"]); v != wantSV {
		t.Errorf("write stamped schema_version = %s, want %s", v, wantSV)
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

// TestMigration_V1ToV2_RewritesClaudeInRequiredPath verifies the
// v1→v2 step: a legacy `required_in_path` array containing "claude"
// gets that entry rewritten to "codex" while preserving every other
// element exactly.
func TestMigration_V1ToV2_RewritesClaudeInRequiredPath(t *testing.T) {
	in := map[string]json.RawMessage{
		"required_in_path": json.RawMessage(`["claude","node","go","bun"]`),
	}
	if err := rewriteRequiredPathClaude(in); err != nil {
		t.Fatalf("rewriteRequiredPathClaude: %v", err)
	}
	var got []string
	if err := json.Unmarshal(in["required_in_path"], &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	want := []string{"codex", "node", "go", "bun"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("required_in_path = %v, want %v", got, want)
	}
}

// TestMigration_V1ToV2_PreservesUserAdditions verifies that customized
// entries beyond the original default set survive the migration: only
// the literal "claude" is rewritten.
func TestMigration_V1ToV2_PreservesUserAdditions(t *testing.T) {
	in := map[string]json.RawMessage{
		"required_in_path": json.RawMessage(`["bun","claude","rust","node"]`),
	}
	if err := rewriteRequiredPathClaude(in); err != nil {
		t.Fatalf("rewriteRequiredPathClaude: %v", err)
	}
	var got []string
	_ = json.Unmarshal(in["required_in_path"], &got)
	want := []string{"bun", "codex", "rust", "node"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("required_in_path = %v, want %v", got, want)
	}
}

// TestMigration_V1ToV2_Idempotent: running the step twice or against
// an already-migrated config is a no-op.
func TestMigration_V1ToV2_Idempotent(t *testing.T) {
	in := map[string]json.RawMessage{
		"required_in_path": json.RawMessage(`["codex","node","go","bun"]`),
	}
	if err := rewriteRequiredPathClaude(in); err != nil {
		t.Fatalf("rewriteRequiredPathClaude: %v", err)
	}
	var got []string
	_ = json.Unmarshal(in["required_in_path"], &got)
	want := []string{"codex", "node", "go", "bun"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("required_in_path = %v, want %v (no-op expected)", got, want)
	}
}

// TestHookTimeout_ZeroIsDefault and TestHookTimeout_ExplicitRespected
// cover both branches of the HookTimeout duration resolver. Zero (and
// negative) values fall back to the package default; positive seconds
// are returned verbatim.
func TestHookTimeout_ZeroIsDefault(t *testing.T) {
	for _, n := range []int{0, -3} {
		c := Config{HookTimeoutSec: n}
		want := time.Duration(DefaultHookTimeoutSec) * time.Second
		if got := c.HookTimeout(); got != want {
			t.Errorf("HookTimeout(%d) = %v, want %v", n, got, want)
		}
	}
}

func TestHookTimeout_ExplicitRespected(t *testing.T) {
	c := Config{HookTimeoutSec: 42}
	if got := c.HookTimeout(); got != 42*time.Second {
		t.Errorf("HookTimeout(42) = %v, want 42s", got)
	}
}

// TestRewriteRequiredPathClaude_MissingKey covers the early-return
// branch when obj has no required_in_path key at all (or an empty raw
// value). A v0/v1 config that never customized the list looks exactly
// like this.
func TestRewriteRequiredPathClaude_MissingKey(t *testing.T) {
	in := map[string]json.RawMessage{} // no required_in_path
	if err := rewriteRequiredPathClaude(in); err != nil {
		t.Fatalf("expected no-op nil, got %v", err)
	}
	if _, present := in["required_in_path"]; present {
		t.Error("step should not invent the key")
	}
}

// TestRewriteRequiredPathClaude_MalformedArray covers the
// "json.Unmarshal into []json.RawMessage failed" branch: the malformed
// value is left untouched (jsonstrict will surface the error on the
// typed Load that follows). The step must not error.
func TestRewriteRequiredPathClaude_MalformedArray(t *testing.T) {
	in := map[string]json.RawMessage{
		"required_in_path": json.RawMessage(`"not-an-array"`),
	}
	original := in["required_in_path"]
	if err := rewriteRequiredPathClaude(in); err != nil {
		t.Fatalf("expected no-op on malformed value, got %v", err)
	}
	if string(in["required_in_path"]) != string(original) {
		t.Errorf("malformed value mutated: got %s", in["required_in_path"])
	}
}

// TestRewriteRequiredPathClaude_NonStringEntries covers the inner-loop
// branch where an individual entry is not a JSON string (defensive). It
// is passed through verbatim.
func TestRewriteRequiredPathClaude_NonStringEntries(t *testing.T) {
	in := map[string]json.RawMessage{
		"required_in_path": json.RawMessage(`["claude",123,{"x":1}]`),
	}
	if err := rewriteRequiredPathClaude(in); err != nil {
		t.Fatalf("step error: %v", err)
	}
	var list []json.RawMessage
	if err := json.Unmarshal(in["required_in_path"], &list); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(list[0]) != `"codex"` {
		t.Errorf("first entry (claude) not rewritten: %s", list[0])
	}
	if string(list[1]) != `123` {
		t.Errorf("non-string entry mutated: %s", list[1])
	}
}

// TestRewriteRequiredPathClaude_EmptyRawValue covers the explicit
// "raw exists but len==0" branch (defensive — Marshal would never
// emit this, but a hand-edited config might).
func TestRewriteRequiredPathClaude_EmptyRawValue(t *testing.T) {
	in := map[string]json.RawMessage{
		"required_in_path": json.RawMessage(``),
	}
	if err := rewriteRequiredPathClaude(in); err != nil {
		t.Fatalf("expected no-op, got %v", err)
	}
}

// TestMigration_V1ToV2_FullPlanRewritesLegacyConfig verifies end-to-
// end behavior via the migrate runner: a v1 config.json with
// `claude` in required_in_path lands at v2 with `codex`, no backup.
func TestMigration_V1ToV2_FullPlanRewritesLegacyConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	legacy := `{
  "schema_version": 1,
  "scrollback_lines": 7777,
  "default_mode": "safe",
  "required_in_path": ["claude","node","go","bun"]
}`
	if err := os.WriteFile(path, []byte(legacy), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := migrate.Run(path, MigrationPlan()); err != nil {
		t.Fatalf("migrate.Run: %v", err)
	}
	raw, _ := os.ReadFile(path)
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var sv int
	_ = json.Unmarshal(obj["schema_version"], &sv)
	if sv != 2 {
		t.Fatalf("schema_version = %d, want 2", sv)
	}
	var got []string
	_ = json.Unmarshal(obj["required_in_path"], &got)
	if !reflect.DeepEqual(got, []string{"codex", "node", "go", "bun"}) {
		t.Fatalf("required_in_path post-migrate = %v", got)
	}
}
