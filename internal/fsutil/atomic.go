// Package fsutil holds tiny, dependency-free filesystem helpers shared
// across the codebase. Currently exports AtomicWriteFile, which replaces
// the three near-identical copies that lived in
// internal/claude/jsonpatch.go, internal/migrate/migrate.go, and
// internal/jsonstrict/jsonstrict.go.
package fsutil

import (
	"fmt"
	"os"
	"path/filepath"
)

// AtomicWriteFile writes data to path via a same-directory temp file
// followed by rename(2), so concurrent readers never observe a
// half-written file. The temp file's mode is forced to perm before close
// to override os.CreateTemp's default 0600 — caller intent wins.
//
// Failure cleanup: on any error after the temp file is created, the temp
// file is removed by the deferred os.Remove. On success the rename
// consumes the temp path so the deferred remove is a no-op.
func AtomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".*")
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
