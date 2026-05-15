package codex

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// discoverBudgetVar is the wall-clock ceiling for DiscoverSessionID.
// Codex typically writes its rollout file ~100–500ms after invocation;
// 5s gives plenty of slack on cold caches without making the post-spawn
// goroutine noticeable. var (not const) so tests can shrink it.
var discoverBudgetVar = 5 * time.Second

// discoverPollVar is the interval between filesystem scans during
// discovery. 100ms keeps latency tight without burning CPU.
var discoverPollVar = 100 * time.Millisecond

// rolloutFilenameRe captures the codex thread UUID from a rollout
// filename. Codex writes files as
//
//	rollout-YYYY-MM-DDTHH-MM-SS-<uuid>.jsonl
//
// under ~/.codex/sessions/YYYY/MM/DD/. The UUID is the trailing
// segment before .jsonl; we anchor on "rollout-" prefix + .jsonl
// suffix to avoid catching unrelated files dropped into the dir.
var rolloutFilenameRe = regexp.MustCompile(`^rollout-\d{4}-\d{2}-\d{2}T\d{2}-\d{2}-\d{2}-([0-9a-fA-F-]+)\.jsonl$`)

// DiscoverSessionID polls ~/.codex/sessions/ for a rollout file whose
// mtime is at or after spawnStart and returns its UUID. Empty + false
// on timeout, missing sessions dir, or any I/O error along the way —
// callers fall back to `codex resume --last` semantics, which is
// strictly less precise but still correct.
//
// Implementation: scan the day-directory matching spawnStart's UTC
// date (plus the previous day if spawnStart is within the first 5
// minutes of UTC midnight, to absorb clock skew). The file whose
// mtime is closest to spawnStart and >= spawnStart wins.
func DiscoverSessionID(spawnStart time.Time) (string, bool) {
	deadline := time.Now().Add(discoverBudgetVar)
	for {
		id, ok := scanForRollout(spawnStart)
		if ok {
			return id, true
		}
		if time.Now().After(deadline) {
			return "", false
		}
		time.Sleep(discoverPollVar)
	}
}

// scanForRollout walks the relevant day directories under
// ~/.codex/sessions/ and returns the UUID of the newest rollout file
// whose mtime is at or after spawnStart. Returns ("", false) when no
// match is found.
func scanForRollout(spawnStart time.Time) (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	root := filepath.Join(home, ".codex", "sessions")

	candidates := dayDirsFor(root, spawnStart.UTC())

	var bestID string
	var bestMtime time.Time
	for _, dir := range candidates {
		if id, mtime, ok := newestMatchingRollout(dir, spawnStart); ok {
			if mtime.After(bestMtime) {
				bestID = id
				bestMtime = mtime
			}
		}
	}
	if bestID == "" {
		return "", false
	}
	return bestID, true
}

// dayDirsFor returns the codex-sessions day directories that could
// contain a rollout file for a spawn at t (UTC). Always includes t's
// own day; also includes the next day when t is within 5 minutes of
// UTC midnight so a clock-skewed file from the rollover doesn't get
// missed.
func dayDirsFor(root string, t time.Time) []string {
	dirs := []string{dayDir(root, t)}
	// Within the first 5 minutes of t's UTC day, codex may have written
	// a file dated for the previous day if its clock is skewed; check
	// there too.
	if t.Hour() == 0 && t.Minute() < 5 {
		dirs = append(dirs, dayDir(root, t.Add(-1*time.Hour)))
	}
	return dirs
}

func dayDir(root string, t time.Time) string {
	return filepath.Join(root,
		t.Format("2006"),
		t.Format("01"),
		t.Format("02"))
}

// newestMatchingRollout walks dir for rollout-named .jsonl files,
// returning the UUID and mtime of the freshest one whose mtime is at
// or after minMtime. Returns ("", _, false) when dir is missing or
// no match is found.
func newestMatchingRollout(dir string, minMtime time.Time) (string, time.Time, bool) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", time.Time{}, false
		}
		return "", time.Time{}, false
	}

	// Sub-second precision in spawnStart vs filesystem mtime can race
	// when tests run rapid-fire; round minMtime down to the second so
	// a file whose mtime equals spawnStart-on-the-tick still matches.
	cutoff := minMtime.Truncate(time.Second)

	var bestID string
	var bestMtime time.Time
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".jsonl") {
			continue
		}
		m := rolloutFilenameRe.FindStringSubmatch(name)
		if len(m) != 2 {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		mtime := info.ModTime()
		if mtime.Before(cutoff) {
			continue
		}
		if mtime.After(bestMtime) {
			bestID = m[1]
			bestMtime = mtime
		}
	}
	if bestID == "" {
		return "", time.Time{}, false
	}
	return bestID, bestMtime, true
}
