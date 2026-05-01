package cmd

import (
	stdbytes "bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// captureStdout redirects os.Stdout for the duration of fn and returns
// everything that was written. Used to assert on side-effecting helpers.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	var (
		buf stdbytes.Buffer
		wg  sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&buf, r)
	}()

	fn()

	w.Close()
	os.Stdout = old
	wg.Wait()
	return buf.String()
}

// withFlags resets logs* flag globals around fn so tests don't leak.
func withFlags(t *testing.T, follow, raw bool, since, tool, grep string, fn func()) {
	t.Helper()
	pf, pr := logsFollow, logsRaw
	ps, pt, pg := logsSince, logsTool, logsGrep
	logsFollow = follow
	logsRaw = raw
	logsSince = since
	logsTool = tool
	logsGrep = grep
	defer func() {
		logsFollow, logsRaw = pf, pr
		logsSince, logsTool, logsGrep = ps, pt, pg
	}()
	fn()
}

// --- compileFilters ---------------------------------------------------------

func TestCompileFilters_AllUnsetIsInactive(t *testing.T) {
	withFlags(t, false, false, "", "", "", func() {
		fs, err := compileFilters()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if fs.active {
			t.Errorf("expected inactive filterSpec, got active=true")
		}
		if fs.grep != nil || fs.toolLow != "" || !fs.since.IsZero() {
			t.Errorf("expected zero spec, got %+v", fs)
		}
	})
}

func TestCompileFilters_AllSetActive(t *testing.T) {
	withFlags(t, false, false, "1h", "Bash", "foo", func() {
		fs, err := compileFilters()
		if err != nil {
			t.Fatalf("err: %v", err)
		}
		if !fs.active {
			t.Error("expected active=true")
		}
		if fs.toolLow != "bash" {
			t.Errorf("toolLow = %q, want bash", fs.toolLow)
		}
		if fs.grep == nil {
			t.Error("expected grep to be compiled")
		}
		if fs.since.IsZero() {
			t.Error("expected since to be set")
		}
	})
}

func TestCompileFilters_BadSince(t *testing.T) {
	withFlags(t, false, false, "abcd", "", "", func() {
		_, err := compileFilters()
		if err == nil || !strings.Contains(err.Error(), "--since") {
			t.Errorf("expected --since error, got %v", err)
		}
	})
}

func TestCompileFilters_BadGrep(t *testing.T) {
	withFlags(t, false, false, "", "", "[invalid(", func() {
		_, err := compileFilters()
		if err == nil || !strings.Contains(err.Error(), "--grep") {
			t.Errorf("expected --grep error, got %v", err)
		}
	})
}

// --- truncate edge ----------------------------------------------------------

func TestTruncate_LongerThanN(t *testing.T) {
	got := truncate("abcdefghij", 5)
	// returns s[:n-1] + "…"  (which is 1 rune, 3 bytes).
	if !strings.HasSuffix(got, "…") {
		t.Errorf("truncate did not append ellipsis: %q", got)
	}
	if !strings.HasPrefix(got, "abcd") {
		t.Errorf("truncate prefix wrong: %q", got)
	}
}

func TestTruncate_ShorterThanN(t *testing.T) {
	if got := truncate("abc", 5); got != "abc" {
		t.Errorf("truncate(abc,5) = %q, want abc", got)
	}
}

// --- toolInputSummary -------------------------------------------------------

func TestToolInputSummary_AllKeyPaths(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want string
	}{
		{"file_path", map[string]any{"file_path": "/tmp/x"}, "/tmp/x"},
		{"path", map[string]any{"path": "/var/log"}, "/var/log"},
		{"command", map[string]any{"command": "echo hi"}, "echo hi"},
		{"pattern", map[string]any{"pattern": "foo.*"}, "foo.*"},
		{"url", map[string]any{"url": "https://x"}, "https://x"},
		{"prompt", map[string]any{"prompt": "hi there"}, "hi there"},
		{"none", map[string]any{"other": "ignored"}, "—"},
		{"non-map", "string-input", "—"},
		{"nil", nil, "—"},
		{"empty string val falls through to none", map[string]any{"file_path": ""}, "—"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := toolInputSummary(c.in); got != c.want {
				t.Errorf("got %q want %q", got, c.want)
			}
		})
	}
}

func TestToolInputSummary_Truncates(t *testing.T) {
	long := strings.Repeat("a", 200)
	got := toolInputSummary(map[string]any{"command": long})
	if !strings.HasSuffix(got, "…") {
		t.Errorf("expected truncated output, got len=%d", len(got))
	}
}

// --- printFormattedEntry ----------------------------------------------------

func TestPrintFormattedEntry_ValidJSON(t *testing.T) {
	out := captureStdout(t, func() {
		printFormattedEntry([]byte(`{"ctm_timestamp":"2026-01-02T03:04:05Z","tool_name":"Bash","tool_input":{"command":"ls"}}`))
	})
	if !strings.Contains(out, "Bash") || !strings.Contains(out, "ls") || !strings.Contains(out, "2026-01-02T03:04:05Z") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestPrintFormattedEntry_MissingFieldsUseDashes(t *testing.T) {
	out := captureStdout(t, func() {
		printFormattedEntry([]byte(`{}`))
	})
	if !strings.Contains(out, "—") || !strings.Contains(out, "?") {
		t.Errorf("expected fallback markers in %q", out)
	}
}

func TestPrintFormattedEntry_InvalidJSONPrintsRaw(t *testing.T) {
	out := captureStdout(t, func() {
		printFormattedEntry([]byte("not-json-at-all"))
	})
	if !strings.Contains(out, "not-json-at-all") {
		t.Errorf("expected raw passthrough, got %q", out)
	}
}

// --- dumpOne / dumpLog formatting paths -------------------------------------

func TestDumpOne_RawAndFormatted(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeLines(t, path, 2)

	// Raw mode: the lines pass through verbatim.
	withFlags(t, false, true, "", "", "", func() {
		out := captureStdout(t, func() {
			if err := dumpOne(path, filterSpec{}); err != nil {
				t.Fatalf("dumpOne: %v", err)
			}
		})
		if strings.Count(out, "\n") < 2 {
			t.Errorf("expected ≥2 lines, got %q", out)
		}
		if !strings.Contains(out, `"tool_name":"Read"`) {
			t.Errorf("raw passthrough missing JSON: %q", out)
		}
	})

	// Formatted mode: prints the table-shaped row, not raw JSON.
	withFlags(t, false, false, "", "", "", func() {
		out := captureStdout(t, func() {
			if err := dumpOne(path, filterSpec{}); err != nil {
				t.Fatalf("dumpOne formatted: %v", err)
			}
		})
		if !strings.Contains(out, "Read") || !strings.Contains(out, "/x") {
			t.Errorf("formatted output missing fields: %q", out)
		}
	})
}

func TestDumpOne_FilterSkipsAll(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sess.jsonl")
	writeLines(t, path, 3)

	fs := filterSpec{
		grep:   regexp.MustCompile("WILL_NOT_MATCH"),
		active: true,
	}
	withFlags(t, false, true, "", "", "", func() {
		out := captureStdout(t, func() {
			if err := dumpOne(path, fs); err != nil {
				t.Fatalf("dumpOne: %v", err)
			}
		})
		if out != "" {
			t.Errorf("expected empty output when filter rejects all, got %q", out)
		}
	})
}

func TestDumpOne_NonexistentReturnsError(t *testing.T) {
	if err := dumpOne(filepath.Join(t.TempDir(), "missing.jsonl"), filterSpec{}); err == nil {
		t.Error("expected error opening nonexistent path")
	}
}

func TestDumpLog_NoSourcesIsNoOp(t *testing.T) {
	// Path inside an empty directory: logrotate.Sources should return
	// an empty slice and dumpLog should succeed without printing.
	dir := t.TempDir()
	withFlags(t, false, true, "", "", "", func() {
		out := captureStdout(t, func() {
			if err := dumpLog(filepath.Join(dir, "ghost.jsonl"), filterSpec{}); err != nil {
				t.Errorf("dumpLog on empty dir should not error, got %v", err)
			}
		})
		if out != "" {
			t.Errorf("expected no output, got %q", out)
		}
	})
}

// --- listSessionLogs --------------------------------------------------------

func TestListSessionLogs_MissingDirIsSoft(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	if err := listSessionLogs(dir); err != nil {
		t.Errorf("missing log dir should not error, got %v", err)
	}
}

func TestListSessionLogs_EmptyDirSoftMessage(t *testing.T) {
	dir := t.TempDir() // empty
	if err := listSessionLogs(dir); err != nil {
		t.Errorf("empty log dir should not error, got %v", err)
	}
}

func TestListSessionLogs_PrintsRows(t *testing.T) {
	dir := t.TempDir()
	writeLines(t, filepath.Join(dir, "session-aaa.jsonl"), 2)
	writeLines(t, filepath.Join(dir, "session-bbb.jsonl"), 1)
	// Touch one to be more recent so sort order is deterministic.
	now := time.Now()
	if err := os.Chtimes(filepath.Join(dir, "session-aaa.jsonl"), now, now); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if err := os.Chtimes(filepath.Join(dir, "session-bbb.jsonl"), now.Add(-time.Hour), now.Add(-time.Hour)); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	// Non-jsonl and a directory should be ignored.
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skip.txt"), []byte("ignored"), 0600); err != nil {
		t.Fatalf("write: %v", err)
	}

	out := captureStdout(t, func() {
		if err := listSessionLogs(dir); err != nil {
			t.Fatalf("listSessionLogs: %v", err)
		}
	})
	if !strings.Contains(out, "SESSION") || !strings.Contains(out, "CALLS") {
		t.Errorf("missing header, out=%q", out)
	}
	if !strings.Contains(out, "session-aaa") || !strings.Contains(out, "session-bbb") {
		t.Errorf("missing session names, out=%q", out)
	}
	if strings.Contains(out, "skip.txt") || strings.Contains(out, "subdir") {
		t.Errorf("non-jsonl entries leaked: %q", out)
	}
	// Newest first → aaa before bbb.
	if strings.Index(out, "session-aaa") > strings.Index(out, "session-bbb") {
		t.Errorf("expected aaa before bbb (newer-first), got %q", out)
	}
}

// --- runLogs (covers HOME-based path resolution + missing-session error) ----

// pointHomeAtTempDir sets $HOME so that config.Dir() resolves under a
// tempdir we control. Returns the resolved logs/ path.
func pointHomeAtTempDir(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	logs := filepath.Join(home, ".config", "ctm", "logs")
	if err := os.MkdirAll(logs, 0700); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	return logs
}

func TestRunLogs_NoArgListsLogs(t *testing.T) {
	logs := pointHomeAtTempDir(t)
	writeLines(t, filepath.Join(logs, "abc.jsonl"), 1)

	withFlags(t, false, false, "", "", "", func() {
		out := captureStdout(t, func() {
			if err := runLogs(&cobra.Command{}, nil); err != nil {
				t.Fatalf("runLogs: %v", err)
			}
		})
		if !strings.Contains(out, "abc") {
			t.Errorf("expected listing to include 'abc', got %q", out)
		}
	})
}

func TestRunLogs_BadSinceReturnsError(t *testing.T) {
	pointHomeAtTempDir(t)
	withFlags(t, false, false, "abcd", "", "", func() {
		err := runLogs(&cobra.Command{}, []string{"sess"})
		if err == nil || !strings.Contains(err.Error(), "--since") {
			t.Errorf("expected --since error, got %v", err)
		}
	})
}

func TestRunLogs_MissingSessionReturnsError(t *testing.T) {
	pointHomeAtTempDir(t)
	withFlags(t, false, false, "", "", "", func() {
		err := runLogs(&cobra.Command{}, []string{"nope"})
		if err == nil || !strings.Contains(err.Error(), "no log file") {
			t.Errorf("expected no-log-file error, got %v", err)
		}
	})
}

func TestRunLogs_DumpsExistingSession(t *testing.T) {
	logs := pointHomeAtTempDir(t)
	writeLines(t, filepath.Join(logs, "sessX.jsonl"), 2)

	withFlags(t, false, true, "", "", "", func() {
		out := captureStdout(t, func() {
			if err := runLogs(&cobra.Command{}, []string{"sessX"}); err != nil {
				t.Fatalf("runLogs: %v", err)
			}
		})
		if strings.Count(out, "\n") < 2 {
			t.Errorf("expected ≥2 raw lines, got %q", out)
		}
	})
}

// --- tailLog (drives the rotation/truncation paths via context cancel) ------

func TestTailLog_DrainsThenExitsOnContextCancel(t *testing.T) {
	logs := pointHomeAtTempDir(t)
	path := filepath.Join(logs, "sessTail.jsonl")
	writeLines(t, path, 2)

	ctx, cancel := context.WithCancel(context.Background())
	root := &cobra.Command{}
	root.SetContext(ctx)

	// wg gates this test on the goroutine fully exiting, including
	// withFlags' deferred restore of the package-level logs* globals.
	// Without this, a follow-on test's withFlags read can race the
	// in-flight defer write — caught by `go test -race`.
	var wg sync.WaitGroup
	done := make(chan error, 1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		withFlags(t, true, true, "", "", "", func() {
			done <- tailLog(root, path, filterSpec{})
		})
	}()

	// Let the initial drain run, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("tailLog returned err on cancel: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("tailLog did not exit after cancel")
	}
	wg.Wait()
}

func TestTailLog_NonexistentPathReturnsError(t *testing.T) {
	ctx := context.Background()
	root := &cobra.Command{}
	root.SetContext(ctx)
	withFlags(t, true, true, "", "", "", func() {
		err := tailLog(root, filepath.Join(t.TempDir(), "missing.jsonl"), filterSpec{})
		if err == nil {
			t.Error("expected error for nonexistent tail path")
		}
	})
}

// --- ensure init registered logsCmd on rootCmd (sanity / cheap coverage) ----

func TestLogsCmdRegistered(t *testing.T) {
	if _, _, err := rootCmd.Find([]string{"logs"}); err != nil {
		t.Fatalf("logs subcommand not registered: %v", err)
	}
}

// --- extra: parseSince is hit indirectly above; this guards the empty
//             string error path stays an error after compileFilters drops
//             through.

func TestParseSince_EmptyIsError(t *testing.T) {
	if _, err := parseSince(""); err == nil {
		t.Error("expected error for empty string")
	}
}

// keep fmt import used (in case future tests are added)
var _ = fmt.Sprintf
