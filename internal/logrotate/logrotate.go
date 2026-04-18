// Package logrotate implements size- and age-bounded rotation for the
// append-only JSONL logs ctm writes under ~/.config/ctm/logs/.
//
// Design choices:
//   - Rotated files use a unix-nanosecond suffix plus ".gz": e.g.
//     "<path>.1745000000000000000.gz". This yields stable chronological
//     ordering without cascading renames and makes age-based pruning a
//     mtime lookup rather than a filename parse.
//   - Rotation runs synchronously in the hook path. At the default
//     50 MiB threshold, gzip is well under 1s on a modern CPU and is
//     triggered at most once per threshold crossing, not per line.
//   - A sibling ".rotate.lock" coordinates concurrent writers so only
//     one rotates at a time; a late caller finds the file already small
//     and no-ops.
//   - The active file's mode is preserved; rotated .gz siblings inherit
//     the same mode. Callers that want 0600 on the active file will
//     automatically get 0600 on .gz siblings too.
package logrotate

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Policy governs rotation and retention. Zero-valued fields disable the
// corresponding check (e.g. MaxSize=0 disables size-based rotation).
type Policy struct {
	MaxSize  int64         // rotate when the active file exceeds this (bytes)
	MaxAge   time.Duration // prune rotated siblings older than this
	MaxFiles int           // cap rotated siblings (keep the newest N)
}

// DefaultPolicy returns the ctm default: rotate at 50 MiB, keep at most
// 30 days of history or 10 files, whichever is smaller.
func DefaultPolicy() Policy {
	return Policy{
		MaxSize:  50 << 20,
		MaxAge:   30 * 24 * time.Hour,
		MaxFiles: 10,
	}
}

// MaybeRotate rotates path if it exceeds p.MaxSize. It renames the file
// to "<path>.<unix-nano>", gzips the result in place, and truncates the
// original to a fresh empty file. After rotation it runs Prune so the
// caller does not have to.
//
// Contract:
//   - Missing file → no-op (nothing to rotate).
//   - p.MaxSize == 0 → no-op (size-based rotation disabled; caller may
//     still invoke Prune separately for age-only retention).
//   - Concurrent calls serialize on a sibling "<path>.rotate.lock"; the
//     second caller re-stats post-lock and no-ops if the file is back
//     under the threshold.
//
// Writers that keep an open fd to path across a rotation continue
// appending to the rotated file (same inode). We accept that small
// trailing-edge data loss (a handful of lines landing in the just-
// rotated file instead of the fresh active) as the cost of not
// coordinating fds across processes; the data is still on disk, just
// in the .gz rather than the active log.
func MaybeRotate(path string, p Policy) error {
	if p.MaxSize <= 0 {
		return nil
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Size() <= p.MaxSize {
		return nil
	}

	lockFile, err := acquireRotateLock(path)
	if err != nil {
		return err
	}
	defer releaseRotateLock(lockFile)

	// Re-stat post-lock: another process may have already rotated.
	info, err = os.Stat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("restat %s: %w", path, err)
	}
	if info.Size() <= p.MaxSize {
		return nil
	}

	perm := info.Mode().Perm()
	stamp := time.Now().UnixNano()
	rotatedRaw := fmt.Sprintf("%s.%d", path, stamp)
	rotatedGz := rotatedRaw + ".gz"

	// Rename swaps the old bytes aside atomically. Writers holding a
	// prior open fd will continue to append to this file until they
	// close; new opens hit the fresh empty file below.
	if err := os.Rename(path, rotatedRaw); err != nil {
		return fmt.Errorf("rename for rotation: %w", err)
	}

	// Recreate the active path as an empty file with the same mode so
	// the subsequent append reopens onto a fresh inode.
	if err := writeEmpty(path, perm); err != nil {
		// Rollback: move the rotated file back so we don't leave the
		// caller without an active log path.
		_ = os.Rename(rotatedRaw, path)
		return fmt.Errorf("recreate active: %w", err)
	}

	if err := gzipInPlace(rotatedRaw, rotatedGz, perm); err != nil {
		// Leave the uncompressed sibling in place rather than risk
		// losing it. Sources() only surfaces .gz, but the user can
		// recover the plain file by hand.
		return fmt.Errorf("gzip rotated file: %w", err)
	}
	if err := os.Remove(rotatedRaw); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove raw rotated file: %w", err)
	}

	return Prune(path, p)
}

// Prune removes rotated siblings of path that violate the retention
// caps in p: older than MaxAge, or beyond MaxFiles (oldest discarded
// first). The active log file itself is never touched by Prune.
//
// Zero-valued caps disable the corresponding check.
func Prune(path string, p Policy) error {
	rotated, err := rotatedSiblings(path)
	if err != nil {
		return err
	}

	if p.MaxAge > 0 {
		cutoff := time.Now().Add(-p.MaxAge)
		kept := rotated[:0]
		for _, rp := range rotated {
			info, err := os.Stat(rp)
			if err != nil {
				continue
			}
			if info.ModTime().Before(cutoff) {
				if err := os.Remove(rp); err != nil && !os.IsNotExist(err) {
					return fmt.Errorf("prune age %s: %w", rp, err)
				}
				continue
			}
			kept = append(kept, rp)
		}
		rotated = kept
	}

	if p.MaxFiles > 0 && len(rotated) > p.MaxFiles {
		// rotatedSiblings returns oldest-first; drop from the front.
		excess := len(rotated) - p.MaxFiles
		for _, rp := range rotated[:excess] {
			if err := os.Remove(rp); err != nil && !os.IsNotExist(err) {
				return fmt.Errorf("prune count %s: %w", rp, err)
			}
		}
	}
	return nil
}

// Sources returns every readable log source belonging to path, in
// chronological order (oldest first). Rotated .gz siblings come first;
// the active path is last. Entries that cannot be stat()ed are dropped.
//
// Missing active file is not an error — Sources only returns rotated
// siblings in that case.
func Sources(path string) ([]string, error) {
	rotated, err := rotatedSiblings(path)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(path); err == nil {
		rotated = append(rotated, path)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat active %s: %w", path, err)
	}
	return rotated, nil
}

// Open returns a reader for a log source. If path ends in ".gz" the
// reader transparently decompresses; otherwise it's a plain file
// reader. The returned ReadCloser owns both the file and the gzip
// reader (if any); a single Close cleans up both.
func Open(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(path, ".gz") {
		return f, nil
	}
	gr, err := gzip.NewReader(f)
	if err != nil {
		f.Close() //nolint:errcheck
		return nil, fmt.Errorf("gzip reader for %s: %w", path, err)
	}
	return &gzipReadCloser{file: f, gz: gr}, nil
}

type gzipReadCloser struct {
	file *os.File
	gz   *gzip.Reader
}

func (g *gzipReadCloser) Read(p []byte) (int, error) { return g.gz.Read(p) }
func (g *gzipReadCloser) Close() error {
	err1 := g.gz.Close()
	err2 := g.file.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

// rotatedSiblings returns paths to rotated .gz files for the active
// path, in chronological order (oldest first based on the nanosecond
// suffix). Any sibling that doesn't match "<path>.<int>.gz" is ignored,
// so unrelated files in the same dir never confuse us.
func rotatedSiblings(path string) ([]string, error) {
	dir := filepath.Dir(path)
	base := filepath.Base(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("readdir %s: %w", dir, err)
	}

	prefix := base + "."
	type rotated struct {
		path  string
		stamp int64
	}
	var out []rotated
	for _, e := range entries {
		name := e.Name()
		if name == base || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".gz") {
			continue
		}
		middle := strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".gz")
		stamp, err := strconv.ParseInt(middle, 10, 64)
		if err != nil {
			continue // unrelated file — caller's naming, not ours
		}
		out = append(out, rotated{
			path:  filepath.Join(dir, name),
			stamp: stamp,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].stamp < out[j].stamp })

	paths := make([]string, len(out))
	for i, r := range out {
		paths[i] = r.path
	}
	return paths, nil
}

// writeEmpty creates or truncates path to an empty file with perm.
func writeEmpty(path string, perm os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	return f.Close()
}

// gzipInPlace compresses src to dst, preserving perm on dst. If dst
// already exists it is overwritten atomically via temp + rename.
func gzipInPlace(src, dst string, perm os.FileMode) error {
	sf, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open src: %w", err)
	}
	defer sf.Close() //nolint:errcheck

	dir := filepath.Dir(dst)
	tmp, err := os.CreateTemp(dir, filepath.Base(dst)+".*")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) //nolint:errcheck

	gw := gzip.NewWriter(tmp)
	if _, err := io.Copy(gw, sf); err != nil {
		gw.Close() //nolint:errcheck
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("gzip copy: %w", err)
	}
	if err := gw.Close(); err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("gzip close: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close() //nolint:errcheck
		return fmt.Errorf("chmod temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpPath, dst); err != nil {
		return fmt.Errorf("rename temp to dst: %w", err)
	}
	return nil
}

// acquireRotateLock opens a sibling lock file and blocks on an
// exclusive flock until it is granted. The returned *os.File must be
// passed to releaseRotateLock.
func acquireRotateLock(path string) (*os.File, error) {
	lockPath := path + ".rotate.lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("open rotate lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		f.Close() //nolint:errcheck
		return nil, fmt.Errorf("flock rotate lock: %w", err)
	}
	return f, nil
}

// releaseRotateLock releases a lock acquired via acquireRotateLock.
// The caller must discard the file handle after.
func releaseRotateLock(f *os.File) {
	if f == nil {
		return
	}
	_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
	_ = f.Close()
}

// sentinel for callers that want to distinguish not-exist from other errors
var errNotExist = errors.New("logrotate: file does not exist")

var _ = errNotExist // reserved for future API
