package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/RandomCodeSpace/ctm/internal/claude"
)

func init() {
	rootCmd.AddCommand(statuslineCmd)
}

// statuslineCmd is the target for claude's statusLine.command setting.
// Claude pipes a JSON payload on stdin every time the status redraws;
// we print a three-line display on stdout. Hidden because it's an
// internal hook, not a user-facing command.
//
// Output layout (3 lines):
//
//	Line 1: <model> · <project>           (project shown as a plain path)
//	Line 2: c 25% (437k)  w 40%  h 10%    (context % + tokens + rate limits)
//	Line 3: ↑ 117k  ↓ 434k                (cumulative session input / output)
//
// Cache_read (⚡) was dropped from the status because its magnitude is
// already captured in the context-tokens parenthesis and Claude Code's
// own focus-mode overlay duplicates the information. Weekly / 5-hour
// rate limits share line 2 with context because they're all
// percentages; tokens share line 3 because both are cumulative ints.
var statuslineCmd = &cobra.Command{
	Use:           "statusline",
	Short:         "Internal statusLine renderer — reads JSON on stdin (hidden)",
	Hidden:        true,
	Args:          cobra.NoArgs,
	RunE:          runStatusline,
	SilenceUsage:  true,
	SilenceErrors: true,
}

// statuslineInput is the subset of claude's statusLine payload we render.
// Pointer fields let us distinguish "field absent" from "field is 0".
type statuslineInput struct {
	// SessionID is Claude's session UUID, used as the {uuid} substitution
	// in CTM_STATUSLINE_DUMP so ctm serve can ingest per-session quota
	// without needing tmux env plumbing.
	SessionID string `json:"session_id"`
	Model     struct {
		DisplayName string `json:"display_name"`
		ID          string `json:"id"`
	} `json:"model"`
	Workspace struct {
		ProjectDir string `json:"project_dir"`
	} `json:"workspace"`
	Cwd           string `json:"cwd"`
	ContextWindow struct {
		UsedPercentage    *float64 `json:"used_percentage"`
		TotalInputTokens  *int64   `json:"total_input_tokens"`
		TotalOutputTokens *int64   `json:"total_output_tokens"`
		CurrentUsage      struct {
			InputTokens              *int64 `json:"input_tokens"`
			CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
		} `json:"current_usage"`
	} `json:"context_window"`
	RateLimits struct {
		SevenDay struct {
			UsedPercentage *float64 `json:"used_percentage"`
		} `json:"seven_day"`
		FiveHour struct {
			UsedPercentage *float64 `json:"used_percentage"`
		} `json:"five_hour"`
	} `json:"rate_limits"`
}

func runStatusline(cmd *cobra.Command, args []string) error {
	data, err := io.ReadAll(io.LimitReader(os.Stdin, 1<<20))
	if err != nil || len(data) == 0 {
		return nil
	}

	// Parse early so the `{uuid}` substitution below has a session ID to
	// work with. Unmarshal failures fall back silently to the legacy
	// no-template behavior so a malformed payload never blocks rendering.
	var in statuslineInput
	parseErr := json.Unmarshal(data, &in)

	// CTM_STATUSLINE_DUMP supports a `{uuid}` template that ctm serve's
	// quota ingest watches for. With the template, each redraw writes
	// `<dir>/<sanitized-uuid>.json` (one file per session). Without the
	// template (legacy behavior), it writes a single global file —
	// global rate limits still work, per-session context % does not.
	if dump := os.Getenv("CTM_STATUSLINE_DUMP"); dump != "" {
		path := dump
		if strings.Contains(path, "{uuid}") {
			uuid := "unknown"
			if parseErr == nil && in.SessionID != "" {
				uuid = sanitizeSessionID(in.SessionID)
			}
			path = strings.ReplaceAll(path, "{uuid}", uuid)
		}
		// Ensure the parent dir exists (the templated default
		// `/tmp/ctm-statusline/{uuid}.json` typically requires this).
		if dir := filepath.Dir(path); dir != "" && dir != "." {
			_ = os.MkdirAll(dir, 0o700)
		}
		_ = os.WriteFile(path, data, 0o600)
	}

	if parseErr != nil {
		return nil
	}
	rendered := renderStatusline(&in)
	if rendered != "" {
		fmt.Println(rendered)
	}
	// Diagnostic twin of CTM_STATUSLINE_DUMP: if CTM_STATUSLINE_OUT
	// points at a path, write the rendered bytes there. Lets a caller
	// cross-reference input-payload dump against the exact output
	// string ctm produced on the same redraw.
	if out := os.Getenv("CTM_STATUSLINE_OUT"); out != "" {
		_ = os.WriteFile(out, []byte(rendered), 0600)
	}
	return nil
}

// Okabe-Ito colorblind-safe palette, matching the original bash script.
const (
	cReset    = "\x1b[0m"
	cCyan     = "\x1b[1;38;5;33m"  // context bar + project header
	cMagenta  = "\x1b[1;38;5;220m" // weekly bar
	cYellow   = "\x1b[1;38;5;208m" // 5-hour bar
	cHdrModel = "\x1b[1;97m"
	cTokIn   = "\x1b[1;38;5;33m"
	cTokOut  = "\x1b[1;38;5;37m"
	cDimGray = "\x1b[90m"
)

func renderStatusline(in *statuslineInput) string {
	var lines []string
	if s := buildHeader(in); s != "" {
		lines = append(lines, s)
	}
	// Line 2: context + rate-limit percentages on one line.
	mid := joinNonEmpty(buildContextLine(in), buildRateLimitLine(in))
	if mid != "" {
		lines = append(lines, mid)
	}
	if s := buildTokenLine(in); s != "" {
		lines = append(lines, s)
	}
	return strings.Join(lines, "\n")
}

// joinNonEmpty joins its arguments with "  " between each pair, skipping
// empty strings. Used to glue together optional statusline segments
// without leaving trailing or leading whitespace when a section was
// skipped for a missing payload field.
func joinNonEmpty(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "  ")
}

func buildHeader(in *statuslineInput) string {
	model := in.Model.DisplayName
	if model == "" {
		model = in.Model.ID
	}
	project := in.Workspace.ProjectDir
	if project == "" {
		project = in.Cwd
	}

	var parts []string
	if model != "" {
		parts = append(parts, formatModel(model))
	}
	if project != "" {
		parts = append(parts, shortenPath(project))
	}

	switch len(parts) {
	case 0:
		return ""
	case 1:
		return cHdrModel + parts[0] + cReset
	default:
		return cHdrModel + parts[0] + cReset + "  " + cCyan + parts[1] + cReset
	}
}

// readEffortLevel is a cmd-package-local wrapper so other renderers can
// pick up the current effort level without duplicating the path dance.
// Silent on every error path — effort is a nice-to-have, not critical.
func readEffortLevel() string {
	p, err := claude.SettingsJSONPath()
	if err != nil {
		return ""
	}
	return claude.ReadEffortLevel(p)
}

// buildContextLine builds the `c <pct>% (<tokens>)` segment of line 2.
// The context-window-used percentage is the primary signal; the
// parenthesised token sum (input + cache_creation + cache_read, per
// Claude Code's input-only formula) is a secondary concrete number.
// Returns "" when used_percentage is absent.
func buildContextLine(in *statuslineInput) string {
	used := in.ContextWindow.UsedPercentage
	if used == nil {
		return ""
	}
	usedPct := int(math.Round(*used))
	entry := fmt.Sprintf("%sc %d%%%s", cCyan, usedPct, cReset)
	if ctx := contextTokens(in); ctx > 0 {
		entry += fmt.Sprintf(" %s(%s)%s", cDimGray, fmtTokens(ctx), cReset)
	}
	return entry
}

// buildTokenLine renders the cumulative session token totals: `↑ <input>`
// and `↓ <output>`. cache_read (⚡) used to live here too; it was dropped
// from the statusline — the token magnitude is already visible as the
// parenthesised number on the context line and Claude Code's focus-mode
// overlay renders its own cache indicator.
func buildTokenLine(in *statuslineInput) string {
	var parts []string
	add := func(glyph rune, color string, n *int64) {
		if n == nil || *n <= 0 {
			return
		}
		parts = append(parts, fmt.Sprintf("%s%c%s %s%s%s",
			color, glyph, cReset, cDimGray, fmtTokens(*n), cReset))
	}
	add('↑', cTokIn, in.ContextWindow.TotalInputTokens)
	add('↓', cTokOut, in.ContextWindow.TotalOutputTokens)
	line := strings.Join(parts, "  ")

	// Tack the current effort level onto the last line. Sourced from
	// ~/.claude/settings.json via readEffortLevel — not in Claude
	// Code's statusLine payload. Dim-gray so it reads as secondary
	// info next to the token counts. Only appended when at least one
	// token is present so a truly empty payload doesn't render a
	// lone "· xhigh" orphan.
	if line == "" {
		return ""
	}
	if effort := readEffortLevel(); effort != "" {
		line += fmt.Sprintf("  %s%s%s", cDimGray, effort, cReset)
	}
	return line
}

// buildRateLimitLine renders `w <pct>%` and `h <pct>%` for weekly and
// 5-hour rate-limit usage. Percentages only — Claude Code's payload
// does not expose token counts for these buckets. Rendered on the
// same physical line as the context percentage (joined by renderStatusline).
func buildRateLimitLine(in *statuslineInput) string {
	var parts []string
	add := func(label rune, color string, used *float64) {
		if used == nil {
			return
		}
		usedPct := int(math.Round(*used))
		parts = append(parts, fmt.Sprintf("%s%c %d%%%s",
			color, label, usedPct, cReset))
	}
	add('w', cMagenta, in.RateLimits.SevenDay.UsedPercentage)
	add('h', cYellow, in.RateLimits.FiveHour.UsedPercentage)
	return strings.Join(parts, "  ")
}

// contextTokens returns the number of tokens currently consumed in the
// context window, computed per Claude Code's documented formula:
//
//	input_tokens + cache_creation_input_tokens + cache_read_input_tokens
//
// (current_usage only, input-side only — this is the same definition
// used to derive context_window.used_percentage; output tokens do not
// count toward context). Any missing field is treated as 0; the sum is
// capped at zero so the caller can branch on >0 to decide whether to
// render.
func contextTokens(in *statuslineInput) int64 {
	cu := in.ContextWindow.CurrentUsage
	var total int64
	if cu.InputTokens != nil {
		total += *cu.InputTokens
	}
	if cu.CacheCreationInputTokens != nil {
		total += *cu.CacheCreationInputTokens
	}
	if cu.CacheReadInputTokens != nil {
		total += *cu.CacheReadInputTokens
	}
	if total < 0 {
		return 0
	}
	return total
}

// formatModel returns the model's display name with redundant words
// stripped. Two simplifications happen:
//
//  1. The "Claude " / "claude-" prefix is dropped (every model in this
//     statusline is a Claude model — the word carries no signal).
//  2. The trailing " context" inside a "(… context)" marker is
//     collapsed so "(1M context)" / "(200K context)" render as "(1M)"
//     / "(200K)". The marker number alone is understood; the word
//     just eats width.
//
// Examples:
//
//	"Claude Opus 4.7 (1M context)"   → "Opus 4.7 (1M)"
//	"Claude Sonnet 4.5 (200K)"        → "Sonnet 4.5 (200K)"
//	"Claude Opus 4.7"                 → "Opus 4.7"
//	"claude-sonnet-4-5-20250929"      → "sonnet-4-5-20250929"
//	""                                → ""
func formatModel(name string) string {
	s := strings.TrimSpace(name)
	if trimmed, ok := strings.CutPrefix(s, "Claude "); ok {
		s = trimmed
	} else if trimmed, ok := strings.CutPrefix(s, "claude-"); ok {
		s = trimmed
	}
	s = strings.Replace(s, " context)", ")", 1)
	return s
}

// shortenPath rewrites $HOME prefix as "~".
func shortenPath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(os.PathSeparator)) {
		return "~" + p[len(home):]
	}
	return p
}

// fmtTokens formats n with an SI-style suffix so the statusline width
// stays bounded regardless of how chatty a session gets. Rules:
//
//   - n <  1 000               → "<n>"        e.g. "500"
//   - n <  1 000 000           → "<n/1k>k"    e.g. "1.2k", "402.6k", "1k"
//   - n <  1 000 000 000       → "<n/1M>M"    e.g. "1.5M", "402.6M", "1M"
//   - n ≥ 1 000 000 000        → "<n/1B>B"    e.g. "4.2B"
//
// Negative values (shouldn't happen for token counts but are defended
// against to keep the hook crash-free) are formatted as the raw int.
// Trailing ".0" is stripped so thousands that land on a round number
// render tight ("402k" rather than "402.0k").
func fmtTokens(n int64) string {
	if n < 0 {
		return strconv.FormatInt(n, 10)
	}
	switch {
	case n < 1_000:
		return strconv.FormatInt(n, 10)
	case n < 1_000_000:
		return humanSI(float64(n)/1_000.0, "k")
	case n < 1_000_000_000:
		return humanSI(float64(n)/1_000_000.0, "M")
	default:
		return humanSI(float64(n)/1_000_000_000.0, "B")
	}
}

// humanSI formats v with one decimal then strips a trailing ".0" so
// round-ish numbers look clean ("5k" not "5.0k", "1.5k" unchanged).
func humanSI(v float64, suffix string) string {
	s := strconv.FormatFloat(v, 'f', 1, 64)
	s = strings.TrimSuffix(s, ".0")
	return s + suffix
}
