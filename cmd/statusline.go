package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(statuslineCmd)
}

// statuslineCmd is the target for claude's statusLine.command setting.
// Claude pipes a JSON payload on stdin every time the status redraws;
// we print a three-line display on stdout. Hidden because it's an
// internal hook, not a user-facing command.
//
// Output layout matches the former bash helper, minus jq/awk/grep:
//
//	Line 1: <model> · <project>    (project is OSC 8 hyperlink if git repo)
//	Line 2: c ━━──  w ━───  h ━━━─  (remaining context / weekly / 5-hour)
//	Line 3: ↑ in · ↓ out · ⚡ cache  (session tokens + cache reads)
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
	Model struct {
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
			CacheReadInputTokens *int64 `json:"cache_read_input_tokens"`
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
	var in statuslineInput
	if err := json.Unmarshal(data, &in); err != nil {
		return nil
	}
	rendered := renderStatusline(&in)
	if rendered != "" {
		fmt.Println(rendered)
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
	cHdrSep   = "\x1b[90m"
	cTokIn    = "\x1b[1;38;5;33m"
	cTokOut   = "\x1b[1;38;5;37m"
	cTokCache = "\x1b[1;38;5;220m"
	cDimGray  = "\x1b[90m"
)

func renderStatusline(in *statuslineInput) string {
	line1 := buildHeader(in)
	barLine := buildBars(in)
	tokLine := buildTokens(in)

	var lines []string
	if line1 != "" {
		lines = append(lines, line1)
	}
	if barLine != "" {
		lines = append(lines, barLine)
	}
	if tokLine != "" {
		lines = append(lines, tokLine)
	}
	return strings.Join(lines, "\n")
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
		parts = append(parts, shortenModel(model))
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
		url := gitRemoteURL(project)
		projSeg := cCyan + parts[1] + cReset
		if url != "" {
			projSeg = fmt.Sprintf("\x1b]8;;%s\x1b\\%s\x1b]8;;\x1b\\", url, projSeg)
		}
		return cHdrModel + parts[0] + cReset + cHdrSep + " · " + cReset + projSeg
	}
}

func buildBars(in *statuslineInput) string {
	var parts []string
	add := func(label rune, color string, used *float64) {
		if used == nil {
			return
		}
		pct := int(math.Round(100 - *used))
		parts = append(parts, fmt.Sprintf("%s%c%s %s", color, label, cReset, makeBar(pct, color)))
	}
	add('c', cCyan, in.ContextWindow.UsedPercentage)
	add('w', cMagenta, in.RateLimits.SevenDay.UsedPercentage)
	add('h', cYellow, in.RateLimits.FiveHour.UsedPercentage)
	return strings.Join(parts, "  ")
}

func buildTokens(in *statuslineInput) string {
	var parts []string
	add := func(glyph rune, color string, n *int64) {
		if n == nil || *n <= 0 {
			return
		}
		parts = append(parts, fmt.Sprintf("%s%c%s %s%s%s", color, glyph, cReset, cDimGray, fmtTokens(*n), cReset))
	}
	add('↑', cTokIn, in.ContextWindow.TotalInputTokens)
	add('↓', cTokOut, in.ContextWindow.TotalOutputTokens)
	add('⚡', cTokCache, in.ContextWindow.CurrentUsage.CacheReadInputTokens)
	return strings.Join(parts, "  ")
}

// shortenModel mimics the bash helper: pick one of sonnet/opus/haiku/flash
// from the model name when possible, and append a "(1M)" / "(200K)" context
// marker except for the default marker per family.
func shortenModel(name string) string {
	lower := strings.ToLower(name)
	short := ""
	for _, kw := range []string{"sonnet", "opus", "haiku", "flash"} {
		if strings.Contains(lower, kw) {
			short = kw
			break
		}
	}
	if short == "" {
		short = strings.TrimSpace(strings.ToLower(stripParens(name)))
	}

	marker := extractContextMarker(name)
	if marker == "" {
		return short
	}
	ml := strings.ToLower(marker)
	skip := false
	switch short {
	case "sonnet", "opus", "haiku":
		skip = ml == "200k"
	case "flash":
		skip = ml == "1m"
	}
	if skip {
		return short
	}
	return short + " (" + strings.ToUpper(marker) + ")"
}

// stripParens removes any "(...)" groups from the string.
func stripParens(s string) string {
	for {
		lp := strings.Index(s, "(")
		if lp < 0 {
			return s
		}
		rp := strings.Index(s[lp:], ")")
		if rp < 0 {
			return s
		}
		s = s[:lp] + s[lp+rp+1:]
	}
}

// extractContextMarker finds the first "<digits>[Mk]" run (case-insensitive).
func extractContextMarker(s string) string {
	i := 0
	for i < len(s) {
		if s[i] >= '0' && s[i] <= '9' {
			j := i
			for j < len(s) && s[j] >= '0' && s[j] <= '9' {
				j++
			}
			if j < len(s) {
				c := s[j]
				if c == 'M' || c == 'm' || c == 'K' || c == 'k' {
					return s[i : j+1]
				}
			}
			i = j
			continue
		}
		i++
	}
	return ""
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

// gitRemoteURL walks up from dir looking for a .git/config and, if found,
// returns the origin remote rewritten to https form. Empty string means
// "no remote to link to"; the caller then renders the project without a
// hyperlink.
func gitRemoteURL(dir string) string {
	if dir == "" {
		return ""
	}
	for i := 0; i < 40; i++ {
		cfg := filepath.Join(dir, ".git", "config")
		if data, err := os.ReadFile(cfg); err == nil {
			return parseOriginURL(string(data))
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
	return ""
}

func parseOriginURL(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	inOrigin := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			inOrigin = line == `[remote "origin"]`
			continue
		}
		if !inOrigin {
			continue
		}
		if !strings.HasPrefix(line, "url") {
			continue
		}
		eq := strings.Index(line, "=")
		if eq < 0 {
			continue
		}
		return normalizeRemoteURL(strings.TrimSpace(line[eq+1:]))
	}
	return ""
}

// normalizeRemoteURL rewrites git@host:path and https forms into a plain
// https URL (with any trailing .git stripped). Returns "" for unknown schemes.
func normalizeRemoteURL(raw string) string {
	raw = strings.TrimSuffix(raw, ".git")
	if strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://") {
		return raw
	}
	if strings.HasPrefix(raw, "git@") {
		rest := strings.TrimPrefix(raw, "git@")
		colon := strings.Index(rest, ":")
		if colon < 0 {
			return ""
		}
		return "https://" + rest[:colon] + "/" + rest[colon+1:]
	}
	return ""
}

// makeBar produces a 4-cell bar: `━` for filled, `─` for empty, colored.
func makeBar(pct int, color string) string {
	const width = 4
	filled := pct * width / 100
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	empty := width - filled
	return color + strings.Repeat("━", filled) + cReset + strings.Repeat("─", empty)
}

// fmtTokens formats n as "X.Yk" for n>=1000, else the raw integer.
func fmtTokens(n int64) string {
	if n >= 1000 {
		return strconv.FormatFloat(float64(n)/1000.0, 'f', 1, 64) + "k"
	}
	return strconv.FormatInt(n, 10)
}
