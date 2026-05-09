package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadClaudeEnv_MissingFileIsZeroValueNoError(t *testing.T) {
	dir := t.TempDir()
	got, err := LoadClaudeEnv(filepath.Join(dir, "absent.json"))
	if err != nil {
		t.Fatalf("missing file should be silent: %v", err)
	}
	if len(got.Env) != 0 {
		t.Errorf("missing file should yield empty Env, got %v", got.Env)
	}
}

func TestLoadClaudeEnv_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude-env.json")
	body := `{
  "_comment": "ctm-managed env vars",
  "env": {
    "CLAUDE_CODE_NO_FLICKER": "1",
    "CTM_STATUSLINE_DUMP": "/tmp/{uuid}.json"
  }
}`
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadClaudeEnv(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Env["CLAUDE_CODE_NO_FLICKER"] != "1" {
		t.Errorf("missing/wrong CLAUDE_CODE_NO_FLICKER: %v", got.Env)
	}
	if got.Env["CTM_STATUSLINE_DUMP"] != "/tmp/{uuid}.json" {
		t.Errorf("{uuid} placeholder not preserved verbatim: %v", got.Env)
	}
	if got.Comment == "" {
		t.Errorf("_comment lost on round-trip")
	}
}

func TestLoadClaudeEnv_MalformedJSONReturnsError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadClaudeEnv(path); err == nil {
		t.Errorf("expected error on malformed JSON")
	}
}

func TestLoadClaudeEnv_RejectsInvalidKey(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []string{
		`{"env":{"BAD KEY":"1"}}`,
		`{"env":{"BAD-KEY":"1"}}`,
		`{"env":{"1LEADING_DIGIT":"1"}}`,
		`{"env":{"with;semicolon":"1"}}`,
	} {
		path := filepath.Join(dir, "bad.json")
		if err := os.WriteFile(path, []byte(bad), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadClaudeEnv(path); err == nil {
			t.Errorf("expected error for %s", bad)
		}
	}
}

func TestShellExports_EmptyIsEmptyString(t *testing.T) {
	if got := (ClaudeEnvFile{}).ShellExports(); got != "" {
		t.Errorf("empty file should produce no exports, got %q", got)
	}
}

func TestShellExports_DeterministicOrder(t *testing.T) {
	f := ClaudeEnvFile{Env: map[string]string{
		"ZED": "z",
		"ALF": "a",
		"MID": "m",
	}}
	got := f.ShellExports()
	want := `export ALF='a' MID='m' ZED='z'`
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestShellExports_QuotesAwkwardValues(t *testing.T) {
	f := ClaudeEnvFile{Env: map[string]string{
		"WITH_SPACE":  "hello world",
		"WITH_QUOTE":  `it's`,
		"WITH_DOLLAR": "$HOME/$PATH",
		"WITH_UUID":   "/tmp/{uuid}.json",
	}}
	got := f.ShellExports()
	wants := []string{
		`WITH_SPACE='hello world'`,
		`WITH_QUOTE='it'\''s'`,
		`WITH_DOLLAR='$HOME/$PATH'`,
		`WITH_UUID='/tmp/{uuid}.json'`,
	}
	for _, w := range wants {
		if !contains(got, w) {
			t.Errorf("expected %q in output, got: %s", w, got)
		}
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
