package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/output"
)

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Tail the log (follow new entries)")
	logsCmd.Flags().BoolVar(&logsRaw, "raw", false, "Print raw JSONL lines without formatting")
	rootCmd.AddCommand(logsCmd)
}

var (
	logsFollow bool
	logsRaw    bool
)

var logsCmd = &cobra.Command{
	Use:   "logs [session-id]",
	Short: "View PostToolUse tool-use logs captured for ctm sessions",
	Long: "Show the tool-use log written by ctm's PostToolUse hook. " +
		"With no argument, lists available session logs. With a session ID, " +
		"prints that session's log. Use -f to tail.",
	Args: cobra.MaximumNArgs(1),
	RunE: runLogs,
}

func runLogs(cmd *cobra.Command, args []string) error {
	logDir := filepath.Join(config.Dir(), "logs")

	// No arg → list available session logs.
	if len(args) == 0 {
		return listSessionLogs(logDir)
	}

	// With arg → show that session's log (tailing if requested).
	sessionID := sanitizeSessionID(args[0])
	logFile := filepath.Join(logDir, sessionID+".jsonl")
	if _, err := os.Stat(logFile); err != nil {
		return fmt.Errorf("no log file for session %q at %s", sessionID, logFile)
	}

	if logsFollow {
		return tailLog(cmd, logFile)
	}
	return dumpLog(logFile)
}

// listSessionLogs prints a table of session-id → entry count.
func listSessionLogs(logDir string) error {
	out := output.Stdout()
	entries, err := os.ReadDir(logDir)
	if err != nil {
		if os.IsNotExist(err) {
			out.Dim("no logs yet at %s", logDir)
			out.Dim("they populate after the first tool call in a ctm session")
			return nil
		}
		return fmt.Errorf("reading log dir: %w", err)
	}

	type row struct {
		name  string
		size  int64
		count int
		mtime time.Time
	}
	var rows []row
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".jsonl")
		rows = append(rows, row{
			name:  name,
			size:  info.Size(),
			count: countLines(filepath.Join(logDir, e.Name())),
			mtime: info.ModTime(),
		})
	}

	if len(rows) == 0 {
		out.Dim("no session logs yet at %s", logDir)
		return nil
	}

	// Sort by most recent
	sort.Slice(rows, func(i, j int) bool { return rows[i].mtime.After(rows[j].mtime) })

	fmt.Printf("%-40s  %6s  %-20s\n", "SESSION", "CALLS", "LAST")
	for _, r := range rows {
		fmt.Printf("%-40s  %6d  %-20s\n",
			truncate(r.name, 40),
			r.count,
			humanDuration(time.Since(r.mtime))+" ago")
	}
	return nil
}

// countLines returns the number of newline-terminated records in a file.
// Returns 0 on any error — this is a display helper, not critical.
func countLines(path string) int {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	var count int
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		count++
	}
	return count
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// dumpLog reads all JSONL lines from path and prints them formatted (or raw
// if --raw was passed).
func dumpLog(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if logsRaw {
			fmt.Println(string(line))
			continue
		}
		printFormattedEntry(line)
	}
	return scanner.Err()
}

// tailLog prints existing entries then follows new ones. Polls the file
// every 500ms. Handles truncation and rotation by reopening when the file
// shrinks below our read offset or disappears entirely. Exits cleanly on
// Ctrl-C via the command context.
func tailLog(cmd *cobra.Command, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { f.Close() }()
	reader := bufio.NewReader(f)
	var offset int64

	drain := func() error {
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				offset += int64(len(line))
				trimmed := strings.TrimRight(string(line), "\n")
				if logsRaw {
					fmt.Println(trimmed)
				} else {
					printFormattedEntry([]byte(trimmed))
				}
			}
			if err == io.EOF {
				return nil
			}
			if err != nil {
				return err
			}
		}
	}

	if err := drain(); err != nil {
		return err
	}

	ctx := cmd.Context()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}

		// Detect rotation/truncation by stat-ing the PATH (not the fd).
		info, statErr := os.Stat(path)
		if statErr != nil {
			// File vanished. Try to reopen; if it's back, drain from 0.
			if nf, openErr := os.Open(path); openErr == nil {
				f.Close()
				f = nf
				reader = bufio.NewReader(f)
				offset = 0
				_ = drain()
			}
			continue
		}

		if info.Size() < offset {
			// Truncated or rotated in place — reset.
			if nf, openErr := os.Open(path); openErr == nil {
				f.Close()
				f = nf
				reader = bufio.NewReader(f)
				offset = 0
			}
		}

		if err := drain(); err != nil {
			return err
		}
	}
}

// printFormattedEntry renders a single JSONL entry as a short line:
//   2026-04-12T10:23:45Z  Read         /path/to/file
func printFormattedEntry(raw []byte) {
	var entry map[string]interface{}
	if err := json.Unmarshal(raw, &entry); err != nil {
		fmt.Println(string(raw))
		return
	}
	ts, _ := entry["ctm_timestamp"].(string)
	if ts == "" {
		ts = "—"
	}
	toolName, _ := entry["tool_name"].(string)
	if toolName == "" {
		toolName = "?"
	}
	summary := toolInputSummary(entry["tool_input"])
	fmt.Printf("%-20s  %-12s  %s\n", ts, toolName, summary)
}

// toolInputSummary extracts a short human-readable hint from tool_input.
// Falls back to "—" if nothing useful is found.
func toolInputSummary(v interface{}) string {
	m, ok := v.(map[string]interface{})
	if !ok {
		return "—"
	}
	// Common keys across tools, in priority order
	for _, key := range []string{"file_path", "path", "command", "pattern", "url", "prompt"} {
		if val, ok := m[key].(string); ok && val != "" {
			return truncate(val, 80)
		}
	}
	return "—"
}
