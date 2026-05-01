package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/logrotate"
	"github.com/RandomCodeSpace/ctm/internal/output"
	"github.com/spf13/cobra"
)

func init() {
	logsCmd.Flags().BoolVarP(&logsFollow, "follow", "f", false, "Tail the log (follow new entries)")
	logsCmd.Flags().BoolVar(&logsRaw, "raw", false, "Print raw JSONL lines without formatting")
	logsCmd.Flags().StringVar(&logsSince, "since", "", "Only show entries newer than this duration (e.g. 7d, 24h, 30m). Days accepted via \"Nd\" suffix.")
	logsCmd.Flags().StringVar(&logsTool, "tool", "", "Only show entries whose tool_name matches (case-insensitive, exact).")
	logsCmd.Flags().StringVar(&logsGrep, "grep", "", "Only show entries whose raw JSON line matches this regex.")
	rootCmd.AddCommand(logsCmd)
}

var (
	logsFollow bool
	logsRaw    bool
	logsSince  string
	logsTool   string
	logsGrep   string
)

// jsonlExt is the per-session log file suffix written by log_tool_use.
const jsonlExt = ".jsonl"

// filterSpec is the compiled form of the logs-command filter flags.
// Zero-valued fields disable the corresponding check, so an empty
// filterSpec passes everything.
type filterSpec struct {
	since   time.Time      // zero = no time filter
	toolLow string         // "" = no tool filter (lowercased)
	grep    *regexp.Regexp // nil = no grep filter
	active  bool           // true if any filter is set (cheap short-circuit)
}

// compileFilters builds a filterSpec from the current flag values.
// Returns an error if any flag is malformed.
func compileFilters() (filterSpec, error) {
	var fs filterSpec
	if logsSince != "" {
		d, err := parseSince(logsSince)
		if err != nil {
			return fs, fmt.Errorf("--since: %w", err)
		}
		fs.since = time.Now().Add(-d)
		fs.active = true
	}
	if logsTool != "" {
		fs.toolLow = strings.ToLower(logsTool)
		fs.active = true
	}
	if logsGrep != "" {
		re, err := regexp.Compile(logsGrep)
		if err != nil {
			return fs, fmt.Errorf("--grep: %w", err)
		}
		fs.grep = re
		fs.active = true
	}
	return fs, nil
}

// parseSince accepts Go's time.ParseDuration format plus a "Nd" day
// shorthand ("7d" → 7*24h). Empty string is an error; use an empty
// --since flag value to disable the filter instead.
func parseSince(s string) (time.Duration, error) {
	if strings.HasSuffix(s, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil {
			return 0, fmt.Errorf("invalid day count %q", s)
		}
		if days < 0 {
			return 0, fmt.Errorf("--since must be non-negative, got %q", s)
		}
		return time.Duration(days) * 24 * time.Hour, nil
	}
	return time.ParseDuration(s)
}

// match returns true if the given raw JSONL line passes every active
// filter in fs. Malformed lines (non-JSON) pass iff --grep matches
// literal bytes and no other filter is active — otherwise they fail
// (we cannot introspect tool_name / ctm_timestamp without a parse).
func (fs filterSpec) match(raw []byte) bool {
	if !fs.active {
		return true
	}
	if fs.grep != nil && !fs.grep.Match(raw) {
		return false
	}
	// Short-circuit: if only --grep is set, we're done.
	if fs.since.IsZero() && fs.toolLow == "" {
		return true
	}
	var entry map[string]any
	if err := json.Unmarshal(raw, &entry); err != nil {
		return false
	}
	if !fs.since.IsZero() {
		ts, _ := entry["ctm_timestamp"].(string)
		parsed, err := time.Parse(time.RFC3339, ts)
		if err != nil || parsed.Before(fs.since) {
			return false
		}
	}
	if fs.toolLow != "" {
		name, _ := entry["tool_name"].(string)
		if strings.ToLower(name) != fs.toolLow {
			return false
		}
	}
	return true
}

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

	// No arg → list available session logs. The filter flags apply
	// only to the dump/tail path; listing is unaffected by them.
	if len(args) == 0 {
		return listSessionLogs(logDir)
	}

	fs, err := compileFilters()
	if err != nil {
		return err
	}

	// With arg → show that session's log (tailing if requested).
	sessionID := sanitizeSessionID(args[0])
	logFile := filepath.Join(logDir, sessionID+jsonlExt)
	if _, err := os.Stat(logFile); err != nil {
		return fmt.Errorf("no log file for session %q at %s", sessionID, logFile)
	}

	if logsFollow {
		return tailLog(cmd, logFile, fs)
	}
	return dumpLog(logFile, fs)
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
		if e.IsDir() || !strings.HasSuffix(e.Name(), jsonlExt) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		name := strings.TrimSuffix(e.Name(), jsonlExt)
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

// countLines returns the number of newline-terminated records across
// the active log and every rotated .gz sibling. Returns 0 on any error
// — this is a display helper, not critical.
func countLines(path string) int {
	sources, err := logrotate.Sources(path)
	if err != nil {
		return 0
	}
	var total int
	for _, src := range sources {
		total += countLinesOne(src)
	}
	return total
}

// countLinesOne counts newline-terminated records in a single source
// (plain or .gz), returning 0 on any error.
func countLinesOne(path string) int {
	r, err := logrotate.Open(path)
	if err != nil {
		return 0
	}
	defer r.Close()
	var count int
	scanner := bufio.NewScanner(r)
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

// dumpLog reads every JSONL line for a session — across rotated .gz
// siblings and the active log, in chronological order — applies the
// filter spec (if any), and prints each passing line formatted (or
// raw if --raw was passed).
//
// Per-source errors (e.g. a corrupt .gz, an I/O error mid-read) are
// logged at WARN level and skipped rather than aborting the rest of
// the session's history. A truncated final line in any source is
// handled naturally by bufio.Scanner, which emits the partial bytes
// as a final token.
func dumpLog(path string, fs filterSpec) error {
	sources, err := logrotate.Sources(path)
	if err != nil {
		return err
	}
	for _, src := range sources {
		if err := dumpOne(src, fs); err != nil {
			slog.Warn("skipping unreadable log source",
				"path", src, "err", err)
			continue
		}
	}
	return nil
}

// dumpOne reads a single source (plain or .gz) line by line, drops
// anything the filter rejects, and prints each survivor via
// printFormattedEntry (or raw passthrough when --raw).
func dumpOne(path string, fs filterSpec) error {
	r, err := logrotate.Open(path)
	if err != nil {
		return err
	}
	defer r.Close()

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if !fs.match(line) {
			continue
		}
		if logsRaw {
			fmt.Println(string(line))
			continue
		}
		printFormattedEntry(line)
	}
	return scanner.Err()
}

// tailLog prints existing entries then follows new ones. First it
// drains every rotated .gz sibling in chronological order, then drains
// the active log, then polls every 500ms for new entries. Each line
// is filtered against fs before being printed. Handles truncation and
// mid-tail rotation by reopening when the active file shrinks below
// our read offset or disappears entirely. Exits cleanly on Ctrl-C via
// the command context.
func tailLog(cmd *cobra.Command, path string, fs filterSpec) error {
	// Drain rotated siblings first — they're immutable and won't grow.
	if sources, err := logrotate.Sources(path); err == nil {
		for _, src := range sources {
			if src == path {
				continue
			}
			_ = dumpOne(src, fs)
		}
	}

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
				if fs.match([]byte(trimmed)) {
					if logsRaw {
						fmt.Println(trimmed)
					} else {
						printFormattedEntry([]byte(trimmed))
					}
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
//
//	2026-04-12T10:23:45Z  Read         /path/to/file
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
