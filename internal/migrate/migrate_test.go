package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newPlanV1 returns a no-op v0→v1 plan. Used across tests so we don't re-spell
// it for every case.
func newPlanV1() Plan {
	return Plan{
		Name:           "test.json",
		CurrentVersion: 1,
		Steps:          []Step{nil}, // v0→v1: stamp version only, no content change
	}
}

func TestRun_MissingFile_NoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "absent.json")

	res, err := Run(path, newPlanV1())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Before != -1 || res.After != -1 {
		t.Errorf("got Result{%d,%d,%q}, want {-1,-1,\"\"}", res.Before, res.After, res.Backup)
	}
	if res.Backup != "" {
		t.Errorf("unexpected backup %q for missing file", res.Backup)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file was created; migrator must not create missing files")
	}
}

func TestRun_Unversioned_StampsAndBacksUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	original := `{"foo":"bar","n":42}`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	res, err := Run(path, newPlanV1())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Before != 0 || res.After != 1 {
		t.Errorf("Result = %+v, want Before=0 After=1", res)
	}

	// Backup must exist and contain the original bytes.
	if res.Backup == "" {
		t.Fatal("expected backup path, got empty")
	}
	backupBytes, err := os.ReadFile(res.Backup)
	if err != nil {
		t.Fatalf("reading backup: %v", err)
	}
	if string(backupBytes) != original {
		t.Errorf("backup = %q, want original %q", backupBytes, original)
	}

	// Migrated file must have schema_version=1 and preserve siblings.
	var got map[string]json.RawMessage
	data, _ := os.ReadFile(path)
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("parsing result: %v", err)
	}
	if v := string(got["schema_version"]); v != "1" {
		t.Errorf("schema_version = %s, want 1", v)
	}
	if v := string(got["foo"]); v != `"bar"` {
		t.Errorf("foo preserved? got %s", v)
	}
	if v := string(got["n"]); v != `42` {
		t.Errorf("n preserved? got %s", v)
	}
}

func TestRun_CurrentVersion_NoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	original := `{"schema_version":1,"foo":"bar"}`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	res, err := Run(path, newPlanV1())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Before != 1 || res.After != 1 {
		t.Errorf("Result = %+v, want Before=1 After=1", res)
	}
	if res.Backup != "" {
		t.Errorf("unexpected backup on no-op: %q", res.Backup)
	}

	// File bytes must be byte-identical.
	after, _ := os.ReadFile(path)
	if string(after) != original {
		t.Errorf("file modified on no-op path:\nwas:  %s\nnow:  %s", original, after)
	}

	// And no .bak.* sibling must have been written.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".bak.") {
			t.Errorf("unexpected backup artifact %s", e.Name())
		}
	}
}

func TestRun_NewerVersion_Refused(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	original := `{"schema_version":99,"foo":"bar"}`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err := Run(path, newPlanV1())
	if err == nil {
		t.Fatal("expected error on newer-than-known schema, got nil")
	}

	// File must be untouched.
	after, _ := os.ReadFile(path)
	if string(after) != original {
		t.Errorf("file modified on error path")
	}
}

func TestRun_InvalidJSON_Errors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	original := `{not json`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err := Run(path, newPlanV1())
	if err == nil {
		t.Fatal("expected parse error, got nil")
	}
	after, _ := os.ReadFile(path)
	if string(after) != original {
		t.Errorf("file modified on parse error")
	}
}

func TestRun_PreservesFileMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	if _, err := Run(path, newPlanV1()); err != nil {
		t.Fatalf("run: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("post-migrate mode = %v, want 0600", mode)
	}
}

func TestRun_MultiStep_AppliesAllInOrder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	original := `{"schema_version":0,"legacy_name":"widget"}`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	plan := Plan{
		Name:           "x.json",
		CurrentVersion: 2,
		Steps: []Step{
			// v0→v1: rename legacy_name to name
			func(obj map[string]json.RawMessage) error {
				if v, ok := obj["legacy_name"]; ok {
					obj["name"] = v
					delete(obj, "legacy_name")
				}
				return nil
			},
			// v1→v2: add "kind":"widget"
			func(obj map[string]json.RawMessage) error {
				obj["kind"] = json.RawMessage(`"widget"`)
				return nil
			},
		},
	}

	res, err := Run(path, plan)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.Before != 0 || res.After != 2 {
		t.Errorf("Result = %+v, want Before=0 After=2", res)
	}

	var got map[string]json.RawMessage
	data, _ := os.ReadFile(path)
	_ = json.Unmarshal(data, &got)
	if string(got["schema_version"]) != "2" {
		t.Errorf("schema_version = %s, want 2", got["schema_version"])
	}
	if string(got["name"]) != `"widget"` {
		t.Errorf("name rename not applied: %s", got["name"])
	}
	if _, still := got["legacy_name"]; still {
		t.Errorf("legacy_name should have been removed")
	}
	if string(got["kind"]) != `"widget"` {
		t.Errorf("kind not stamped: %s", got["kind"])
	}
}

func TestRun_StepError_LeavesFileUntouched(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	original := `{"foo":"bar"}`
	if err := os.WriteFile(path, []byte(original), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	plan := Plan{
		Name:           "x.json",
		CurrentVersion: 1,
		Steps: []Step{
			func(obj map[string]json.RawMessage) error {
				return &migrationErr{msg: "boom"}
			},
		},
	}

	_, err := Run(path, plan)
	if err == nil {
		t.Fatal("expected step error, got nil")
	}

	after, _ := os.ReadFile(path)
	if string(after) != original {
		t.Errorf("file modified on step error:\nwas: %s\nnow: %s", original, after)
	}
}

func TestRun_StepCountMismatch_Errors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}

	// CurrentVersion=2 but only one Step supplied — mismatched Plan.
	plan := Plan{
		Name:           "x.json",
		CurrentVersion: 2,
		Steps:          []Step{nil},
	}

	_, err := Run(path, plan)
	if err == nil {
		t.Fatal("expected validation error on step-count mismatch, got nil")
	}
}

// migrationErr is a stable error type for step-error tests.
type migrationErr struct{ msg string }

func (e *migrationErr) Error() string { return e.msg }

// TestRun_NullJSON_TreatedAsEmpty covers the `obj == nil` re-init
// branch: literal `null` parses cleanly into a nil map; the runner
// must replace it with an empty map and proceed (stamping
// schema_version like an unversioned file).
func TestRun_NullJSON_TreatedAsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	if err := os.WriteFile(path, []byte(`null`), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	res, err := Run(path, newPlanV1())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Before != 0 || res.After != 1 {
		t.Errorf("Result = %+v, want Before=0 After=1", res)
	}
}

// TestRun_NonIntegerSchemaVersion_Errors covers the
// "schema_version is not an integer" parse branch — a hand-edited
// config that wrote `"schema_version":"1"` (string) must surface as
// a clear error, not be silently re-stamped.
func TestRun_NonIntegerSchemaVersion_Errors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.json")
	if err := os.WriteFile(path, []byte(`{"schema_version":"one"}`), 0600); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if _, err := Run(path, newPlanV1()); err == nil {
		t.Fatal("expected error on non-integer schema_version")
	}
}
