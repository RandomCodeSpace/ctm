package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// runCompletion invokes `ctm completion <shell>` in-process and returns
// its stdout + error.
func runCompletion(t *testing.T, shell string) (string, error) {
	t.Helper()

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"completion", shell})
	err := rootCmd.Execute()
	rootCmd.SetArgs(nil)
	rootCmd.SetOut(nil)
	rootCmd.SetErr(nil)

	return out.String(), err
}

func TestCompletion_EmitsPerShellScript(t *testing.T) {
	cases := []struct {
		shell string
		hint  string // a string that must appear in the script to prove it's the right one
	}{
		{"bash", "complete"},
		{"zsh", "compdef"},
		{"fish", "complete -c"},
		{"powershell", "Register-ArgumentCompleter"},
	}
	for _, tc := range cases {
		t.Run(tc.shell, func(t *testing.T) {
			got, err := runCompletion(t, tc.shell)
			if err != nil {
				t.Fatalf("completion %s: %v", tc.shell, err)
			}
			if !strings.Contains(got, tc.hint) {
				t.Errorf("%s completion missing hint %q:\n---\n%s\n---", tc.shell, tc.hint, got)
			}
		})
	}
}

func TestCompletion_RejectsUnknownShell(t *testing.T) {
	_, err := runCompletion(t, "not-a-shell")
	if err == nil {
		t.Fatal("expected error for unknown shell, got nil")
	}
}
