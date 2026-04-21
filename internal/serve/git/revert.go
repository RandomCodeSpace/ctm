package git

import (
	"bufio"
	"fmt"
	"strings"
)

// RevertResult is the JSON payload returned by a successful revert.
// `StashedAs` is omitted from the output when the caller did not ask
// for (or did not need) a pre-revert stash.
type RevertResult struct {
	OK         bool   `json:"ok"`
	RevertedTo string `json:"reverted_to"`
	StashedAs  string `json:"stashed_as,omitempty"`
}

// DirtyError is returned by Revert when the workdir has uncommitted
// changes and the caller did not opt in to `stashFirst`. Files holds
// the relative paths reported by `git status --porcelain`.
type DirtyError struct {
	Files []string
}

// Error implements error.
func (e *DirtyError) Error() string {
	if e == nil {
		return "workdir is dirty"
	}
	return fmt.Sprintf("workdir is dirty (%d file(s))", len(e.Files))
}

// Revert resets workdir's HEAD to sha (`git reset --hard <sha>`).
//
// If the workdir is dirty:
//   - !stashFirst → returns *DirtyError (no side effects).
//   - stashFirst  → `git stash push -u -m "ctm-revert pre-<sha>"`,
//     captures the stash commit SHA into RevertResult.StashedAs, then
//     proceeds with the reset. If reset itself fails the stash is left
//     in place for manual recovery — we do not auto-pop.
//
// SECURITY CONTRACT: this function does NOT validate that sha refers
// to a known checkpoint. The caller (the HTTP handler in api/revert.go)
// is responsible for enforcing the SHA-allowlist guarantee from the
// spec — callers that bypass that allowlist allow arbitrary
// `git reset --hard` against the repo. Do not call Revert from any
// path that has not first cross-checked sha against the matching
// `/checkpoints` response.
func Revert(workdir, sha string, stashFirst bool) (RevertResult, error) {
	var res RevertResult

	if !hasGitDir(workdir) {
		return res, fmt.Errorf("workdir %q is not a git repository", workdir)
	}
	if strings.TrimSpace(sha) == "" {
		return res, fmt.Errorf("sha must not be empty")
	}

	status, err := runGit(workdir, "status", "--porcelain")
	if err != nil {
		return res, fmt.Errorf("git status: %w", err)
	}
	dirty := parseDirtyFiles(status)
	if len(dirty) > 0 {
		if !stashFirst {
			return res, &DirtyError{Files: dirty}
		}
		stashMsg := fmt.Sprintf("ctm-revert pre-%s", sha)
		if _, err := runGit(workdir, "stash", "push", "-u", "-m", stashMsg); err != nil {
			return res, fmt.Errorf("git stash: %w", err)
		}
		stashSHA, err := runGit(workdir, "rev-parse", "stash@{0}")
		if err != nil {
			return res, fmt.Errorf("git rev-parse stash@{0}: %w", err)
		}
		res.StashedAs = strings.TrimSpace(stashSHA)
	}

	if _, err := runGit(workdir, "reset", "--hard", sha); err != nil {
		// Surface the underlying git error verbatim. The stash, if any,
		// is intentionally not popped — the user can `git stash pop`.
		return res, fmt.Errorf("git reset --hard %s: %w", sha, err)
	}

	res.OK = true
	res.RevertedTo = sha
	return res, nil
}

// parseDirtyFiles extracts the path portion of every line in
// `git status --porcelain` output. Porcelain v1 format is "XY path"
// (with two status columns and a single space separator). Lines that
// don't match the expected shape are skipped silently.
func parseDirtyFiles(porcelain string) []string {
	var files []string
	scanner := bufio.NewScanner(strings.NewReader(porcelain))
	for scanner.Scan() {
		line := scanner.Text()
		if len(line) < 4 {
			continue
		}
		// Skip the two status columns and the separating space.
		path := strings.TrimSpace(line[3:])
		if path == "" {
			continue
		}
		// Renames are reported as "old -> new"; surface the new path.
		if idx := strings.Index(path, " -> "); idx >= 0 {
			path = path[idx+len(" -> "):]
		}
		files = append(files, path)
	}
	return files
}
