package cmd

import (
	"encoding/json"
	"strings"
	"testing"
)

func intPtr(v int64) *int64     { return &v }
func floatPtr(v float64) *float64 { return &v }

func TestRenderStatuslineFullPayload(t *testing.T) {
	in := &statuslineInput{}
	in.Model.DisplayName = "Claude Sonnet 4.5 (1M)"
	in.Workspace.ProjectDir = "/tmp/ctm-statusline-fake"
	in.ContextWindow.UsedPercentage = floatPtr(25)
	in.ContextWindow.TotalInputTokens = intPtr(12345)
	in.ContextWindow.TotalOutputTokens = intPtr(6789)
	in.ContextWindow.CurrentUsage.CacheReadInputTokens = intPtr(500)
	in.RateLimits.SevenDay.UsedPercentage = floatPtr(40)
	in.RateLimits.FiveHour.UsedPercentage = floatPtr(10)

	out := renderStatusline(in)
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d:\n%s", len(lines), out)
	}
	// Header: short model + project
	if !strings.Contains(lines[0], "sonnet (1M)") {
		t.Errorf("header missing shortened model: %q", lines[0])
	}
	if !strings.Contains(lines[0], "ctm-statusline-fake") {
		t.Errorf("header missing project tail: %q", lines[0])
	}
	// Bars: labels c/w/h
	for _, want := range []string{"c", "w", "h", "━", "─"} {
		if !strings.Contains(lines[1], want) {
			t.Errorf("bars missing %q: %q", want, lines[1])
		}
	}
	// Tokens: 12345 rendered as 12.3k, 6789 as 6.8k, 500 as 500
	for _, want := range []string{"↑", "↓", "⚡", "12.3k", "6.8k", "500"} {
		if !strings.Contains(lines[2], want) {
			t.Errorf("token line missing %q: %q", want, lines[2])
		}
	}
}

func TestRenderStatuslineSkipsMissingFields(t *testing.T) {
	in := &statuslineInput{}
	in.Model.ID = "opus-4.7"
	// No project, no tokens, no bars.
	out := renderStatusline(in)
	if out == "" {
		t.Fatal("expected at least the model line")
	}
	if strings.Contains(out, "\n") {
		t.Errorf("expected exactly one line, got:\n%s", out)
	}
	if !strings.Contains(out, "opus") {
		t.Errorf("expected opus model, got %q", out)
	}
}

func TestRenderStatuslineDefaultMarkerSkipped(t *testing.T) {
	// Sonnet's default 200K marker should be elided.
	if got := shortenModel("Claude Sonnet 4.5 (200K)"); got != "sonnet" {
		t.Errorf("want 'sonnet' (marker elided), got %q", got)
	}
	// Opus 1M should retain the marker.
	if got := shortenModel("Opus 4.7 (1M)"); got != "opus (1M)" {
		t.Errorf("want 'opus (1M)', got %q", got)
	}
	// Flash 1M is default, elided.
	if got := shortenModel("Flash 1M"); got != "flash" {
		t.Errorf("want 'flash', got %q", got)
	}
}

func TestNormalizeRemoteURL(t *testing.T) {
	cases := map[string]string{
		"git@github.com:foo/bar.git":    "https://github.com/foo/bar",
		"https://github.com/foo/bar":    "https://github.com/foo/bar",
		"https://gitlab.com/x/y.git":    "https://gitlab.com/x/y",
		"ftp://nope":                    "",
	}
	for in, want := range cases {
		if got := normalizeRemoteURL(in); got != want {
			t.Errorf("normalizeRemoteURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseOriginURL(t *testing.T) {
	cfg := `[core]
	repositoryformatversion = 0
[remote "upstream"]
	url = git@github.com:elsewhere/repo.git
[remote "origin"]
	url = git@github.com:me/repo.git
	fetch = +refs/heads/*:refs/remotes/origin/*
`
	got := parseOriginURL(cfg)
	if got != "https://github.com/me/repo" {
		t.Errorf("parseOriginURL = %q", got)
	}
}

func TestRunStatuslineTolerantJSON(t *testing.T) {
	// Unmarshal of an empty object must not panic; output should be empty.
	var in statuslineInput
	if err := json.Unmarshal([]byte(`{}`), &in); err != nil {
		t.Fatal(err)
	}
	if out := renderStatusline(&in); out != "" {
		t.Errorf("expected empty output for empty payload, got %q", out)
	}
}
