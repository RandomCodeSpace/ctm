package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/logrotate"
)

func init() {
	rootCmd.AddCommand(logToolUseCmd)
}

// logToolUseCmd is the PostToolUse hook target. Claude invokes it for every
// tool call and pipes the raw hook JSON on stdin. We parse it, add a
// timestamp, and append one JSONL line to ~/.config/ctm/logs/<session>.jsonl.
//
// Hidden because it's an internal hook, not a user-facing command. Always
// exits 0 — hook failures must never block the tool pipeline.
var logToolUseCmd = &cobra.Command{
	Use:    "log-tool-use",
	Short:  "Internal PostToolUse hook — logs tool invocations (hidden)",
	Hidden: true,
	RunE:   runLogToolUse,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// sessionIDSafe matches only characters we allow in a log filename.
// Claude's session IDs are UUIDs, but we sanitize defensively to prevent
// path traversal or filesystem weirdness if the hook payload is malformed.
var sessionIDSafe = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func sanitizeSessionID(id string) string {
	clean := sessionIDSafe.ReplaceAllString(id, "-")
	if clean == "" || len(clean) > 128 {
		return "unknown"
	}
	return clean
}

func runLogToolUse(cmd *cobra.Command, args []string) error {
	// Read all of stdin. Hook payloads are small (<100KB typically).
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20)) // 1 MiB cap
	if err != nil || len(data) == 0 {
		return nil
	}

	// Parse into a generic map so we preserve all fields claude sends.
	var payload map[string]interface{}
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil
	}

	// Extract and sanitize session_id for the filename.
	sessionID := "unknown"
	if v, ok := payload["session_id"].(string); ok && v != "" {
		sessionID = sanitizeSessionID(v)
	}

	// Add a ctm-side timestamp so the log is readable even if claude
	// doesn't include one in the payload.
	payload["ctm_timestamp"] = time.Now().UTC().Format(time.RFC3339)

	logDir := filepath.Join(config.Dir(), "logs")
	// 0700 on the dir — tool payloads can contain file paths and contents.
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return nil
	}
	logFile := filepath.Join(logDir, sessionID+".jsonl")

	line, err := json.Marshal(payload)
	if err != nil {
		return nil
	}
	line = append(line, '\n')

	// 0600 on the file — same reasoning as the dir.
	f, err := os.OpenFile(logFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		return nil
	}

	// Acquire an exclusive advisory lock before writing. O_APPEND is only
	// atomic up to PIPE_BUF (4096 bytes) on Linux; tool payloads can easily
	// exceed that (Read/Bash output). Without the lock, concurrent hook
	// invocations can interleave bytes and corrupt the JSONL stream.
	// Lock failure is non-fatal — write anyway rather than block the tool pipeline.
	fd := int(f.Fd())
	lockAcquired := syscall.Flock(fd, syscall.LOCK_EX) == nil

	_, _ = f.Write(line)

	// Release the advisory lock and close explicitly *before* calling
	// MaybeRotate: rotation takes its own sibling lock, and keeping our
	// fd open across the rename would anchor writers to the rotated
	// inode instead of the fresh active file.
	if lockAcquired {
		_ = syscall.Flock(fd, syscall.LOCK_UN)
	}
	_ = f.Close()

	_ = logrotate.MaybeRotate(logFile, hookRotatePolicy())
	return nil
}

// hookRotatePolicy resolves the rotation policy to use from inside the
// hook. It loads config.json if present; on any error it falls back to
// the built-in defaults so a misconfigured file never blocks a tool
// invocation.
func hookRotatePolicy() logrotate.Policy {
	cfg, err := config.Load(config.ConfigPath())
	if err != nil {
		return logrotate.DefaultPolicy()
	}
	return cfg.LogPolicy()
}
