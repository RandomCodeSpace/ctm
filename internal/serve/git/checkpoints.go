// Package git provides git-backed primitives for ctm serve: listing
// the YOLO checkpoint commits in a session's workdir and reverting to
// one of them. All operations shell out to the system `git` binary
// with a bounded timeout.
package git

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// gitTimeout caps every shell-out to git. 10s matches the contract in
// the implementation plan and is generous for any non-pathological
// workdir.
const gitTimeout = 10 * time.Second

// maxLimit caps the number of checkpoints returned by List even if the
// caller asks for more. Mirrors the upper bound in the spec's
// `?limit=` query semantics.
const maxLimit = 200

// Checkpoint is the JSON view of a single YOLO checkpoint commit. Tags
// match §6 "Checkpoint JSON" exactly.
type Checkpoint struct {
	SHA     string `json:"sha"`
	Subject string `json:"subject"`
	TS      string `json:"ts"`
	Ago     string `json:"ago"`
}

// List returns up to limit checkpoints from workdir, newest first.
// limit is capped at maxLimit. A missing workdir or one without a
// `.git` directory yields (nil, nil) so the caller can render an
// empty list without surfacing an error to the user.
func List(workdir string, limit int) ([]Checkpoint, error) {
	if !hasGitDir(workdir) {
		return nil, nil
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > maxLimit {
		limit = maxLimit
	}

	out, err := runGit(workdir,
		"log",
		"--grep=checkpoint",
		"--pretty=format:%H%x09%s%x09%cI",
		fmt.Sprintf("-n%d", limit),
	)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	var checkpoints []Checkpoint
	scanner := bufio.NewScanner(strings.NewReader(out))
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			continue
		}
		sha, subject, isoTS := parts[0], parts[1], parts[2]
		// `--grep=checkpoint` is a loose substring match; restrict to
		// commits whose subject actually starts with "checkpoint:".
		if !strings.HasPrefix(subject, "checkpoint:") {
			continue
		}
		ts, terr := time.Parse(time.RFC3339, isoTS)
		if terr != nil {
			// Skip rather than fail the whole list — a single corrupt
			// commit shouldn't blank the UI.
			continue
		}
		checkpoints = append(checkpoints, Checkpoint{
			SHA:     sha,
			Subject: subject,
			TS:      ts.UTC().Format(time.RFC3339),
			Ago:     humaniseAgo(now.Sub(ts)),
		})
	}
	return checkpoints, nil
}

// hasGitDir reports whether workdir is a non-empty path containing a
// `.git` entry (file or directory — covers both standard repos and
// linked worktrees).
func hasGitDir(workdir string) bool {
	if workdir == "" {
		return false
	}
	info, err := os.Stat(workdir)
	if err != nil || !info.IsDir() {
		return false
	}
	if _, err := os.Stat(filepath.Join(workdir, ".git")); err != nil {
		return false
	}
	return true
}

// runGit executes `git -C workdir <args>` with the package-level
// timeout. Returns stdout on success; on failure, returns an error
// that includes stderr for diagnosis.
func runGit(workdir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	full := append([]string{"-C", workdir}, args...)
	cmd := exec.CommandContext(ctx, "git", full...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
		}
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, msg)
	}
	return string(out), nil
}

// humaniseAgo renders a duration as a short human string in the same
// style as GitHub timestamps: "12s", "5m", "3h", "2d", "4w", "9mo",
// "2y". Negative durations clamp to "0s".
func humaniseAgo(d time.Duration) string {
	if d < 0 {
		return "0s"
	}
	s := int64(d.Seconds())
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm", s/60)
	case s < 86400:
		return fmt.Sprintf("%dh", s/3600)
	case s < 7*86400:
		return fmt.Sprintf("%dd", s/86400)
	case s < 30*86400:
		return fmt.Sprintf("%dw", s/(7*86400))
	case s < 365*86400:
		return fmt.Sprintf("%dmo", s/(30*86400))
	default:
		return fmt.Sprintf("%dy", s/(365*86400))
	}
}
