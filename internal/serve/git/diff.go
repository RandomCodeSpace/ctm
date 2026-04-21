package git

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// diffTimeout caps every `git show` shell-out. 5 s per V18 spec —
// tighter than the 10 s gitTimeout shared by List/Revert because the
// diff endpoint is called interactively from the UI and we'd rather
// surface a fast error than hang the sheet.
const diffTimeout = 5 * time.Second

// DiffAt returns the unified diff (patch + commit header) for sha in
// workdir, produced by `git -C <workdir> show --unified=3 <sha>`.
//
// A missing workdir or one without a `.git` entry returns an error —
// unlike List, which falls through to (nil, nil). The diff handler's
// 404 semantics are driven by the SHA-allowlist check upstream, so a
// bad workdir here is unexpected and should surface, not silently
// produce an empty string.
//
// The caller is expected to have already validated sha through
// api.CheckpointsCache.IsCheckpoint — this function shells out
// without re-validation. Do not call with arbitrary user input.
func DiffAt(workdir, sha string) (string, error) {
	if !hasGitDir(workdir) {
		return "", fmt.Errorf("workdir %q is not a git repository", workdir)
	}
	if strings.TrimSpace(sha) == "" {
		return "", fmt.Errorf("sha must not be empty")
	}

	ctx, cancel := context.WithTimeout(context.Background(), diffTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", workdir, "show", "--unified=3", sha)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			return "", fmt.Errorf("git show %s: %w", sha, err)
		}
		return "", fmt.Errorf("git show %s: %w: %s", sha, err, msg)
	}
	return string(out), nil
}
