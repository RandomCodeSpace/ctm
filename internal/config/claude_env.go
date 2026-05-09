package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// ClaudeEnvFile is the on-disk shape of ~/.config/ctm/claude-env.json.
//
// ctm exports these vars into the shell that spawns claude. This is the
// canonical home for env vars claude reads too early in startup for the
// overlay's `env` block to apply (e.g., CLAUDE_CODE_NO_FLICKER). Most
// env vars belong in claude-overlay.json's `env` block instead.
//
// The Comment field is JSON-convention `_comment` and is preserved on
// round-trip but otherwise unused.
type ClaudeEnvFile struct {
	Comment string            `json:"_comment,omitempty"`
	Env     map[string]string `json:"env"`
}

// envKeyRe matches POSIX-portable shell variable names: leading letter or
// underscore, then letters/digits/underscores. Anything else (spaces,
// dashes, shell metachars) would be a corrupt file and we refuse to
// emit shell from it.
var envKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// LoadClaudeEnv reads claude-env.json from path. Returns the zero
// ClaudeEnvFile and a nil error when the file does not exist — a missing
// file is treated as "no extra env vars to export," same graceful
// degradation the previous env.sh sourcing had.
//
// Returns a non-nil error on:
//   - a present but malformed JSON file (loud failure: corrupt config
//     should not silently drop env vars)
//   - any env key that is not a portable shell variable name
//
// On error the returned ClaudeEnvFile is the zero value.
func LoadClaudeEnv(path string) (ClaudeEnvFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ClaudeEnvFile{}, nil
		}
		return ClaudeEnvFile{}, fmt.Errorf("reading %s: %w", path, err)
	}
	var f ClaudeEnvFile
	if err := json.Unmarshal(data, &f); err != nil {
		return ClaudeEnvFile{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	for k := range f.Env {
		if !envKeyRe.MatchString(k) {
			return ClaudeEnvFile{}, fmt.Errorf("%s: invalid env key %q (must match [A-Za-z_][A-Za-z0-9_]*)", path, k)
		}
	}
	return f, nil
}

// ShellExports returns a single-line `export KEY1='val1' KEY2='val2'`
// clause suitable for prepending to the claude launch command. Returns
// "" when there are no entries so callers can branch on emptiness
// without producing a stray "export " in the shell.
//
// Keys are emitted alphabetically so the launch command is deterministic
// across runs (handy for diffing process trees and tests).
//
// Values are wrapped in single quotes with embedded single quotes
// escaped as '\'' — the standard POSIX-safe quoting that handles
// arbitrary characters including spaces, $, `, !, and the literal
// `{uuid}` placeholder consumed downstream by `ctm statusline`.
func (f ClaudeEnvFile) ShellExports() string {
	if len(f.Env) == 0 {
		return ""
	}
	keys := make([]string, 0, len(f.Env))
	for k := range f.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("export ")
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(shellQuoteValue(f.Env[k]))
	}
	return b.String()
}

// shellQuoteValue wraps s in single quotes, escaping embedded single
// quotes as '\''. Mirrors internal/claude.shellQuote — kept local here
// to avoid an import cycle (config is leaf, claude depends on config).
func shellQuoteValue(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ClaudeEnvExports is the one-call convenience: load the file at
// ClaudeEnvPath() and return its ShellExports. Errors are swallowed —
// returning empty string preserves the legacy env.sh behavior of
// "missing file is fine" while letting the caller stay one-liner clean.
//
// Callers that want loud failure on a malformed file should call
// LoadClaudeEnv directly.
func ClaudeEnvExports() string {
	f, err := LoadClaudeEnv(ClaudeEnvPath())
	if err != nil {
		return ""
	}
	return f.ShellExports()
}
