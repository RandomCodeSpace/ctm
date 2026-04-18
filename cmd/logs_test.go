package cmd

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

// writeLine writes n JSONL lines of a minimal tool-use shape to path.
func writeLines(t *testing.T, path string, n int) {
	t.Helper()
	var buf bytes.Buffer
	for i := 0; i < n; i++ {
		buf.WriteString(`{"tool_name":"Read","tool_input":{"file_path":"/x"}}` + "\n")
	}
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// writeGzLines writes n JSONL lines gzipped to path.
func writeGzLines(t *testing.T, path string, n int) {
	t.Helper()
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	for i := 0; i < n; i++ {
		gw.Write([]byte(`{"tool_name":"Read","tool_input":{"file_path":"/x"}}` + "\n"))
	}
	if err := gw.Close(); err != nil {
		t.Fatalf("gzip close: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0600); err != nil {
		t.Fatalf("write gz: %v", err)
	}
}

func TestCountLines_SumsAcrossRotated(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "sess.jsonl")
	writeLines(t, active, 2)
	writeGzLines(t, filepath.Join(dir, "sess.jsonl.1000.gz"), 3)
	writeGzLines(t, filepath.Join(dir, "sess.jsonl.2000.gz"), 4)

	got := countLines(active)
	if got != 2+3+4 {
		t.Errorf("countLines = %d, want 9", got)
	}
}

func TestCountLines_IgnoresUnrelatedSiblings(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "sess.jsonl")
	writeLines(t, active, 2)

	// Unrelated files in the same dir must not be counted.
	writeLines(t, filepath.Join(dir, "other.jsonl"), 10)
	writeGzLines(t, filepath.Join(dir, "sess.jsonl.bak.gz"), 99) // non-numeric suffix
	writeLines(t, filepath.Join(dir, "sess.jsonl.garbage"), 99)  // not .gz

	got := countLines(active)
	if got != 2 {
		t.Errorf("countLines = %d, want 2 (only active)", got)
	}
}

func TestCountLines_ActiveMissingStillSumsRotated(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "sess.jsonl") // never created
	writeGzLines(t, filepath.Join(dir, "sess.jsonl.1.gz"), 5)

	if got := countLines(active); got != 5 {
		t.Errorf("countLines = %d, want 5", got)
	}
}

// TestCountLines_TruncatedFinalLineNotPanics: an active log whose final
// line is a truncated (non-newline-terminated) JSONL record must still
// count and must not crash. bufio.Scanner emits the partial bytes as a
// final token — we count 2 here (one clean + one truncated).
func TestCountLines_TruncatedFinalLineNotPanics(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "sess.jsonl")
	raw := `{"tool_name":"Read","tool_input":{"file_path":"/x"}}` + "\n" +
		`{"tool_name":"Bash","tool_input":{"command":"echo ` // no trailing quote + no newline
	if err := os.WriteFile(active, []byte(raw), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	if got := countLines(active); got != 2 {
		t.Errorf("countLines = %d, want 2 (1 clean + 1 truncated partial)", got)
	}
}

// TestCountLines_CorruptGzSkipsWithoutCrash: a rotated sibling that
// claims to be .gz but isn't (empty or garbage) must be counted as
// zero lines, not crash, and not prevent other sources from counting.
func TestCountLines_CorruptGzSkipsWithoutCrash(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "sess.jsonl")
	writeLines(t, active, 3)
	writeGzLines(t, filepath.Join(dir, "sess.jsonl.1000.gz"), 5)
	// 2000.gz is NOT a gzip file despite the extension.
	if err := os.WriteFile(filepath.Join(dir, "sess.jsonl.2000.gz"), []byte("definitely not gzip"), 0600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	// The good sources should still contribute; the corrupt one counts 0.
	got := countLines(active)
	if got != 3+5 {
		t.Errorf("countLines = %d, want 8 (skip corrupt, sum others)", got)
	}
}

// TestDumpLog_CorruptSourceSkipsAndContinues: dumpLog encountering a
// corrupt .gz should log a warning and continue reading the rest,
// rather than aborting.
func TestDumpLog_CorruptSourceSkipsAndContinues(t *testing.T) {
	dir := t.TempDir()
	active := filepath.Join(dir, "sess.jsonl")
	writeLines(t, active, 2)
	// Corrupt rotated sibling claims .gz but isn't.
	if err := os.WriteFile(filepath.Join(dir, "sess.jsonl.1000.gz"), []byte("garbage"), 0600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}

	if err := dumpLog(active, filterSpec{}); err != nil {
		t.Errorf("dumpLog should not fail on corrupt .gz, got: %v", err)
	}
}

func TestParseSince(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"30m", 30 * time.Minute, false},
		{"24h", 24 * time.Hour, false},
		{"1h30m", 90 * time.Minute, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"0d", 0, false},
		{"365d", 365 * 24 * time.Hour, false},
		{"-3d", 0, true},
		{"abcd", 0, true},
		{"d", 0, true},
		{"", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseSince(tt.in)
			if tt.wantErr {
				if err == nil {
					t.Errorf("parseSince(%q) expected error, got %v", tt.in, got)
				}
				return
			}
			if err != nil {
				t.Errorf("parseSince(%q) err = %v", tt.in, err)
			}
			if got != tt.want {
				t.Errorf("parseSince(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestFilterSpec_MatchesByTool(t *testing.T) {
	fs := filterSpec{toolLow: "bash", active: true}
	cases := []struct {
		name string
		line string
		want bool
	}{
		{"match lower", `{"tool_name":"bash"}`, true},
		{"match mixed case", `{"tool_name":"Bash"}`, true},
		{"different tool", `{"tool_name":"Read"}`, false},
		{"no tool_name", `{"other":"x"}`, false},
		{"invalid json", `not json`, false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := fs.match([]byte(tt.line))
			if got != tt.want {
				t.Errorf("match(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}

func TestFilterSpec_MatchesByGrep(t *testing.T) {
	re := regexp.MustCompile(`(?i)urgent`)
	fs := filterSpec{grep: re, active: true}
	if !fs.match([]byte(`{"msg":"Urgent task"}`)) {
		t.Error("expected case-insensitive grep match")
	}
	if fs.match([]byte(`{"msg":"calm"}`)) {
		t.Error("expected grep to reject non-matching line")
	}
}

func TestFilterSpec_MatchesBySince(t *testing.T) {
	recent := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
	old := time.Now().Add(-10 * 24 * time.Hour).Format(time.RFC3339)

	fs := filterSpec{since: time.Now().Add(-2 * time.Hour), active: true}

	if !fs.match([]byte(fmt.Sprintf(`{"ctm_timestamp":%q}`, recent))) {
		t.Error("recent entry should pass 2h since filter")
	}
	if fs.match([]byte(fmt.Sprintf(`{"ctm_timestamp":%q}`, old))) {
		t.Error("10-day-old entry should fail 2h since filter")
	}
	if fs.match([]byte(`{}`)) {
		t.Error("missing/malformed ctm_timestamp should fail since filter")
	}
}

func TestFilterSpec_Combines(t *testing.T) {
	recent := time.Now().Add(-30 * time.Minute).Format(time.RFC3339)
	fs := filterSpec{
		toolLow: "bash",
		since:   time.Now().Add(-1 * time.Hour),
		active:  true,
	}
	// Match: recent + tool=bash.
	ok := fs.match([]byte(fmt.Sprintf(
		`{"ctm_timestamp":%q,"tool_name":"Bash"}`, recent)))
	if !ok {
		t.Error("recent Bash entry should pass combined filter")
	}
	// Fail: recent but wrong tool.
	fail := fs.match([]byte(fmt.Sprintf(
		`{"ctm_timestamp":%q,"tool_name":"Read"}`, recent)))
	if fail {
		t.Error("recent Read entry should fail combined (tool mismatch)")
	}
}

func TestFilterSpec_ZeroMatchesEverything(t *testing.T) {
	var fs filterSpec // zero value, active=false
	lines := []string{`{"tool_name":"Read"}`, `not json`, `{}`, ``}
	for _, l := range lines {
		if !fs.match([]byte(l)) {
			t.Errorf("zero filter must accept %q", l)
		}
	}
}
