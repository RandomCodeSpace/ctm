package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RandomCodeSpace/ctm/internal/config"
)

// withTempHome is defined in bootstrap_test.go in this package; reuse it.

func TestSessionLogDir(t *testing.T) {
	home := withTempHome(t)
	got := sessionLogDir()
	want := filepath.Join(home, ".config", "ctm", "logs")
	if got != want {
		t.Errorf("sessionLogDir() = %q, want %q", got, want)
	}
}

func TestCtmSubcommand(t *testing.T) {
	// Happy path: os.Executable returns a real path during `go test`, so the
	// returned string must contain the subcommand suffix.
	got := ctmSubcommand("statusline")
	if !strings.HasSuffix(got, " statusline") {
		t.Errorf("ctmSubcommand(\"statusline\") = %q, expected suffix %q", got, " statusline")
	}
	if got == "" {
		t.Error("ctmSubcommand returned empty string")
	}
}

func TestLogToolUseHookCommand(t *testing.T) {
	got := logToolUseHookCommand()
	if !strings.HasSuffix(got, " log-tool-use") {
		t.Errorf("logToolUseHookCommand() = %q, expected suffix %q", got, " log-tool-use")
	}
}

func TestStatuslineHookCommand(t *testing.T) {
	got := statuslineHookCommand()
	if !strings.HasSuffix(got, " statusline") {
		t.Errorf("statuslineHookCommand() = %q, expected suffix %q", got, " statusline")
	}
}

func TestBuildSampleOverlayContainsHookPaths(t *testing.T) {
	got := buildSampleOverlay("/usr/local/bin/ctm statusline", "/usr/local/bin/ctm log-tool-use")

	wants := []string{
		`"reduceMotion": false`,
		`"spinnerTipsEnabled": false`,
		`"statusLine"`,
		`"/usr/local/bin/ctm statusline"`,
		`"/usr/local/bin/ctm log-tool-use"`,
		`"theme": "dark"`,
		`"CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS": "1"`,
		`"PostToolUse"`,
		`"matcher": "*"`,
	}
	for _, want := range wants {
		if !strings.Contains(got, want) {
			t.Errorf("buildSampleOverlay output missing %q\n--- got ---\n%s", want, got)
		}
	}
}

func TestBuildSampleOverlayEscapesPathsWithSpaces(t *testing.T) {
	// %q in fmt.Sprintf is what protects us from a path containing a quote
	// character — verify the JSON stays parseable.
	got := buildSampleOverlay(`/path with spaces/ctm statusline`, `/another path/ctm log-tool-use`)
	if !strings.Contains(got, `"/path with spaces/ctm statusline"`) {
		t.Errorf("statusline path not properly quoted:\n%s", got)
	}
	if !strings.Contains(got, `"/another path/ctm log-tool-use"`) {
		t.Errorf("log hook path not properly quoted:\n%s", got)
	}
}

func TestWriteEnvFileCreatesAndIsIdempotent(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "nested", "dir", "env.sh")

	if err := writeEnvFile(path); err != nil {
		t.Fatalf("first writeEnvFile: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after first write: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("env.sh perm = %v, want 0600", mode)
	}

	// User edit must survive a second call (O_EXCL bails out on EEXIST).
	userEdit := []byte("# user edit\nexport FOO=bar\n")
	if err := os.WriteFile(path, userEdit, 0600); err != nil {
		t.Fatalf("user edit: %v", err)
	}
	if err := writeEnvFile(path); err != nil {
		t.Fatalf("second writeEnvFile: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(userEdit) {
		t.Errorf("user edit clobbered:\nwant:\n%s\ngot:\n%s", userEdit, got)
	}
}

func TestWriteEnvFileMkdirAllErrorPath(t *testing.T) {
	// Pointing the env file at a path whose parent is a regular file forces
	// MkdirAll to fail, exercising the error-return branch.
	tmp := t.TempDir()
	regularFile := filepath.Join(tmp, "blocker")
	if err := os.WriteFile(regularFile, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(regularFile, "child", "env.sh")

	if err := writeEnvFile(target); err == nil {
		t.Errorf("expected error when parent path component is a regular file")
	}
}

func TestRunOverlayStatusNoOverlay(t *testing.T) {
	withTempHome(t)
	if err := runOverlayStatus(nil, nil); err != nil {
		t.Errorf("runOverlayStatus with no overlay returned err: %v", err)
	}
}

func TestRunOverlayStatusWithOverlay(t *testing.T) {
	withTempHome(t)
	// Create overlay + env file so both info-branches are walked.
	if err := os.MkdirAll(config.Dir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ClaudeOverlayPath(), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.EnvFilePath(), []byte("# env\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := runOverlayStatus(nil, nil); err != nil {
		t.Errorf("runOverlayStatus with overlay returned err: %v", err)
	}
}

func TestRunOverlayInitCreates(t *testing.T) {
	withTempHome(t)
	if err := runOverlayInit(nil, nil); err != nil {
		t.Fatalf("runOverlayInit: %v", err)
	}

	overlay := config.ClaudeOverlayPath()
	data, err := os.ReadFile(overlay)
	if err != nil {
		t.Fatalf("reading overlay: %v", err)
	}
	got := string(data)
	for _, want := range []string{
		`"reduceMotion"`,
		`"spinnerTipsEnabled"`,
		`statusline`,
		`log-tool-use`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("overlay missing %q in output:\n%s", want, got)
		}
	}

	info, err := os.Stat(overlay)
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0600 {
		t.Errorf("overlay mode = %v, want 0600", mode)
	}

	// env file + log dir should also exist.
	if _, err := os.Stat(config.EnvFilePath()); err != nil {
		t.Errorf("env file not created: %v", err)
	}
	if st, err := os.Stat(sessionLogDir()); err != nil || !st.IsDir() {
		t.Errorf("session log dir not a directory: %v", err)
	}
}

func TestRunOverlayInitErrorsWhenAlreadyExists(t *testing.T) {
	withTempHome(t)
	if err := os.MkdirAll(config.Dir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.ClaudeOverlayPath(), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}

	err := runOverlayInit(nil, nil)
	if err == nil {
		t.Fatal("runOverlayInit should error when overlay exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error %q should mention 'already exists'", err.Error())
	}
}

// fakeEditorPath writes a minimal POSIX shell editor that exits 0 without
// touching the file, and prepends its directory to PATH for the test.
// The returned editor name is suitable for $EDITOR.
func fakeEditorPath(t *testing.T, name string) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, name)
	// Use #!/bin/sh true-equivalent so the editor exits cleanly.
	script := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(bin, []byte(script), 0700); err != nil {
		t.Fatalf("writing fake editor: %v", err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("EDITOR", name)
}

func TestRunOverlayEditCreatesSampleAndRunsEditor(t *testing.T) {
	withTempHome(t)
	fakeEditorPath(t, "fake-editor-create")

	if err := runOverlayEdit(nil, nil); err != nil {
		t.Fatalf("runOverlayEdit: %v", err)
	}

	// Sample overlay should be created on first edit.
	data, err := os.ReadFile(config.ClaudeOverlayPath())
	if err != nil {
		t.Fatalf("reading overlay: %v", err)
	}
	if !strings.Contains(string(data), "statusLine") {
		t.Errorf("expected sample overlay content, got:\n%s", data)
	}
	if _, err := os.Stat(config.EnvFilePath()); err != nil {
		t.Errorf("expected env file, got err: %v", err)
	}
}

func TestRunOverlayEditExistingFile(t *testing.T) {
	withTempHome(t)
	fakeEditorPath(t, "fake-editor-existing")

	if err := os.MkdirAll(config.Dir(), 0700); err != nil {
		t.Fatal(err)
	}
	preexisting := []byte(`{"theme":"light"}`)
	if err := os.WriteFile(config.ClaudeOverlayPath(), preexisting, 0600); err != nil {
		t.Fatal(err)
	}

	if err := runOverlayEdit(nil, nil); err != nil {
		t.Fatalf("runOverlayEdit: %v", err)
	}

	// Editor exits without changes; existing content must be preserved.
	got, err := os.ReadFile(config.ClaudeOverlayPath())
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(preexisting) {
		t.Errorf("existing overlay was rewritten\nwant: %s\ngot:  %s", preexisting, got)
	}
}

func TestRunOverlayEditMissingEditor(t *testing.T) {
	withTempHome(t)
	// Empty PATH + nonexistent editor name -> exec.LookPath fails.
	t.Setenv("PATH", "")
	t.Setenv("EDITOR", "definitely-not-a-real-editor-xyzzy")

	err := runOverlayEdit(nil, nil)
	if err == nil {
		t.Fatal("expected error when editor is missing")
	}
	if !strings.Contains(err.Error(), "not found in PATH") {
		t.Errorf("error %q should mention editor not found in PATH", err.Error())
	}

	// Half-created sample must NOT exist (resolver runs before any FS work).
	if _, statErr := os.Stat(config.ClaudeOverlayPath()); statErr == nil {
		t.Error("overlay file should not have been created when editor lookup failed")
	}
}

func TestRunOverlayEditDefaultsToVi(t *testing.T) {
	withTempHome(t)
	// Unset $EDITOR to exercise the "EDITOR == empty -> vi" branch. With an
	// empty PATH, vi resolution will fail and we get a clear error mentioning
	// "vi".
	t.Setenv("PATH", "")
	t.Setenv("EDITOR", "")

	err := runOverlayEdit(nil, nil)
	if err == nil {
		t.Fatal("expected error when vi missing from empty PATH")
	}
	if !strings.Contains(err.Error(), `"vi"`) {
		t.Errorf("expected error to name default editor vi, got: %v", err)
	}
}

func TestOverlayPathCmdRunE(t *testing.T) {
	withTempHome(t)
	// Exercise the inline RunE on overlayPathCmd. It calls fmt.Println and
	// returns nil — no observable state beyond no-error.
	if err := overlayPathCmd.RunE(overlayPathCmd, nil); err != nil {
		t.Errorf("overlayPathCmd RunE returned err: %v", err)
	}
}
