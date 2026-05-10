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
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d:\n%s", len(lines), out)
	}
	if !strings.Contains(lines[0], "Sonnet 4.5 (1M)") {
		t.Errorf("header missing full model name: %q", lines[0])
	}
	if !strings.Contains(lines[0], "ctm-statusline-fake") {
		t.Errorf("header missing project tail: %q", lines[0])
	}
	for _, want := range []string{"ctx", "25%", "w", "40%", "h", "10%"} {
		if !strings.Contains(lines[1], want) {
			t.Errorf("line 2 missing %q: %q", want, lines[1])
		}
	}
	if strings.Contains(lines[1], "(") || strings.Contains(lines[1], ")") {
		t.Errorf("line 2 should not contain token-count parens: %q", lines[1])
	}
	for _, banned := range []string{"⚡", "↑", "↓"} {
		if strings.Contains(out, banned) {
			t.Errorf("dropped glyph %q should not appear:\n%s", banned, out)
		}
	}
	for _, bar := range []string{"━", "─"} {
		if strings.Contains(out, bar) {
			t.Errorf("unexpected bar rune %q in output:\n%s", bar, out)
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

func TestFormatModel(t *testing.T) {
	cases := map[string]string{
		"Claude Sonnet 4.5 (1M)":             "Sonnet 4.5 (1M)",
		"Claude Sonnet 4.5 (200K)":           "Sonnet 4.5 (200K)",
		"Claude Opus 4.7 (1M context)":       "Opus 4.7 (1M)",
		"Claude Sonnet 4.5 (1M context)":     "Sonnet 4.5 (1M)",
		"Claude Sonnet 4.5 (200K context)":   "Sonnet 4.5 (200K)",
		"Claude Opus 4.7":                    "Opus 4.7",
		"claude-sonnet-4-5-20250929":         "sonnet-4-5-20250929",
		"claude-opus-4-7":                    "opus-4-7",
		"Opus 4.7 (1M)":                      "Opus 4.7 (1M)", // no "Claude " prefix — unchanged
		"  Claude Sonnet 4.5  ":              "Sonnet 4.5",
		"":                                   "",
	}
	for in, want := range cases {
		if got := formatModel(in); got != want {
			t.Errorf("formatModel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestContextTokens_SumsCurrentUsage(t *testing.T) {
	in := &statuslineInput{}
	in.ContextWindow.CurrentUsage.InputTokens = intPtr(1000)
	in.ContextWindow.CurrentUsage.CacheCreationInputTokens = intPtr(2000)
	in.ContextWindow.CurrentUsage.CacheReadInputTokens = intPtr(437270)
	if got := contextTokens(in); got != 1000+2000+437270 {
		t.Errorf("contextTokens = %d, want %d", got, 1000+2000+437270)
	}
}

func TestContextTokens_NilFieldsTreatedAsZero(t *testing.T) {
	in := &statuslineInput{}
	// Only cache_read is present — input and cache_creation are nil.
	in.ContextWindow.CurrentUsage.CacheReadInputTokens = intPtr(500)
	if got := contextTokens(in); got != 500 {
		t.Errorf("contextTokens with two nils = %d, want 500", got)
	}
}

func TestContextTokens_AllNilReturnsZero(t *testing.T) {
	in := &statuslineInput{}
	if got := contextTokens(in); got != 0 {
		t.Errorf("contextTokens with empty current_usage = %d, want 0", got)
	}
}

func TestFmtTokens(t *testing.T) {
	cases := map[int64]string{
		0:              "0",
		1:              "1",
		500:            "500",
		999:            "999",
		1_000:          "1k",
		1_234:          "1.2k",
		12_345:         "12.3k",
		100_000:        "100k",
		402_620:        "402.6k",
		999_999:        "1000k", // just under the M cutoff
		1_000_000:      "1M",
		1_500_000:      "1.5M",
		402_620_000:    "402.6M",
		999_999_999:    "1000M",
		1_000_000_000:  "1B",
		4_250_000_000:  "4.2B", // round-half-to-even: 4.25 → 4.2
	}
	for in, want := range cases {
		if got := fmtTokens(in); got != want {
			t.Errorf("fmtTokens(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFmtTokens_NegativeRendersRaw(t *testing.T) {
	if got := fmtTokens(-42); got != "-42" {
		t.Errorf("fmtTokens(-42) = %q, want %q", got, "-42")
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
