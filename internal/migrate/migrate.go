// Package migrate applies versioned upgrades to ctm's JSON state files.
//
// Each ctm-owned state file (config.json, sessions.json) carries a
// "schema_version" integer at its top level. On startup, ctm runs a
// Plan against each file: if the file is below the current version, it
// applies the pending Steps in order, stamps the new version, and
// atomically writes the result. The original bytes are saved to a
// timestamped sibling (".bak.<unix-nano>") before any destructive write
// so downgrade/recovery remains possible.
//
// The runner is intentionally conservative:
//   - It never creates a missing file (the owning package decides lifecycle).
//   - It preserves the existing file mode on write.
//   - It refuses to touch a file whose schema_version exceeds the current
//     target, so an older ctm binary won't silently mangle state written by
//     a newer binary (downgrade guard).
package migrate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Step migrates a JSON object in-place from version v to v+1, where v is
// the index of the Step in Plan.Steps. A nil Step is a no-op (useful for
// version bumps that only need the stamp).
type Step func(obj map[string]json.RawMessage) error

// Plan describes the target version and the Steps to reach it from v0.
// len(Steps) must equal CurrentVersion: Steps[i] migrates vi → v(i+1).
type Plan struct {
	Name           string // used in error messages only
	CurrentVersion int
	Steps          []Step
}

// Result records what Run observed and did.
//
// Before: the schema_version read from the file. -1 means the file did
// not exist. 0 means the file existed but lacked a schema_version key.
// After:  the schema_version written. -1 when no write occurred (either
// because the file was absent or because it was already at CurrentVersion).
// Backup: path to the pre-migration backup, or "" when none was written.
type Result struct {
	Before int
	After  int
	Backup string
}

// Run applies p to the file at path. See package doc for semantics.
func Run(path string, p Plan) (Result, error) {
	if len(p.Steps) != p.CurrentVersion {
		return Result{}, fmt.Errorf("migrate: plan %q has %d steps but CurrentVersion=%d (must match)", p.Name, len(p.Steps), p.CurrentVersion)
	}

	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return Result{Before: -1, After: -1}, nil
	}
	if err != nil {
		return Result{}, fmt.Errorf("migrate %s: stat: %w", p.Name, err)
	}

	original, err := os.ReadFile(path)
	if err != nil {
		return Result{}, fmt.Errorf("migrate %s: read: %w", p.Name, err)
	}

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(original, &obj); err != nil {
		return Result{}, fmt.Errorf("migrate %s: parse: %w", p.Name, err)
	}
	if obj == nil {
		obj = map[string]json.RawMessage{}
	}

	from := 0
	if raw, ok := obj["schema_version"]; ok {
		if err := json.Unmarshal(raw, &from); err != nil {
			return Result{}, fmt.Errorf("migrate %s: schema_version is not an integer: %s", p.Name, string(raw))
		}
	}

	if from == p.CurrentVersion {
		return Result{Before: from, After: from}, nil
	}
	if from > p.CurrentVersion {
		return Result{Before: from}, fmt.Errorf("migrate %s: file schema_version=%d exceeds known version %d; refusing to downgrade", p.Name, from, p.CurrentVersion)
	}

	for v := from; v < p.CurrentVersion; v++ {
		step := p.Steps[v]
		if step == nil {
			continue
		}
		if err := step(obj); err != nil {
			return Result{Before: from}, fmt.Errorf("migrate %s: v%d→v%d: %w", p.Name, v, v+1, err)
		}
	}

	stampBytes, err := json.Marshal(p.CurrentVersion)
	if err != nil {
		return Result{Before: from}, fmt.Errorf("migrate %s: stamp version: %w", p.Name, err)
	}
	obj["schema_version"] = stampBytes

	out, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return Result{Before: from}, fmt.Errorf("migrate %s: marshal: %w", p.Name, err)
	}

	perm := info.Mode().Perm()
	backupPath := path + ".bak." + strconv.FormatInt(time.Now().UnixNano(), 10)
	if err := os.WriteFile(backupPath, original, perm); err != nil {
		return Result{Before: from}, fmt.Errorf("migrate %s: write backup: %w", p.Name, err)
	}

	if err := atomicWriteFile(path, out, perm); err != nil {
		return Result{Before: from, Backup: backupPath}, fmt.Errorf("migrate %s: write: %w", p.Name, err)
	}

	return Result{Before: from, After: p.CurrentVersion, Backup: backupPath}, nil
}

// atomicWriteFile writes data to path via a temp file in the same directory
// followed by rename(2). Mirrors internal/claude/jsonpatch.go; duplicated
// rather than extracted here to keep the migrate package self-contained.
// A follow-up commit can consolidate both into internal/fsutil if a third
// caller appears.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	base := filepath.Base(path) + ".*"
	tmp, err := os.CreateTemp(dir, base)
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) //nolint:errcheck

	if _, err := tmp.Write(data); err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("rename temp: %w", err)
	}
	return nil
}
