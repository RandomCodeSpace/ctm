package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
	"github.com/RandomCodeSpace/ctm/internal/config"
	"github.com/RandomCodeSpace/ctm/internal/output"
)

func init() {
	overlayCmd.AddCommand(overlayInitCmd)
	overlayCmd.AddCommand(overlayEditCmd)
	overlayCmd.AddCommand(overlayPathCmd)
	rootCmd.AddCommand(overlayCmd)
}

var overlayCmd = &cobra.Command{
	Use:   "overlay",
	Short: "Manage the ctm-only claude settings overlay",
	Long: "The overlay file at ~/.config/ctm/claude-overlay.json contains claude " +
		"settings (statusline, theme, etc.) that apply ONLY when claude is launched " +
		"via ctm. Direct `claude` invocations are unaffected.",
	RunE: runOverlayStatus,
}

var overlayInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create a sample overlay file with statusline + theme examples",
	RunE:  runOverlayInit,
}

var overlayEditCmd = &cobra.Command{
	Use:   "edit",
	Short: "Open the overlay file in $EDITOR (creates it first if missing)",
	RunE:  runOverlayEdit,
}

var overlayPathCmd = &cobra.Command{
	Use:   "path",
	Short: "Print the overlay file path",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println(config.ClaudeOverlayPath())
		return nil
	},
}

// statusScriptPath returns the path to the ctm status-line helper script.
func statusScriptPath() string {
	return filepath.Join(config.Dir(), "statusline.sh")
}

// sessionLogDir returns the directory where per-session tool-use logs are written.
func sessionLogDir() string {
	return filepath.Join(config.Dir(), "logs")
}

// logToolUseHookCommand returns the shell command claude invokes as the
// PostToolUse hook. We use `ctm log-tool-use` (the hidden subcommand in
// cmd/log_tool_use.go) so parsing is native Go — no jq / bash dependency.
//
// We resolve the ctm binary's absolute path at overlay generation time so
// the hook works even if PATH changes. Falls back to plain "ctm
// log-tool-use" if os.Executable fails, which is rare.
func logToolUseHookCommand() string {
	if exe, err := os.Executable(); err == nil && exe != "" {
		return exe + " log-tool-use"
	}
	return "ctm log-tool-use"
}

// sampleStatusScript is the bash helper invoked by claude's statusLine feature.
// It reads JSON on stdin and prints a 3-line status display:
//
//	Line 1: <model> · <project>    (project is OSC 8 hyperlink if git repo)
//	Line 2: c ━━──  w ━───  h ━━━─  (context / weekly / 5hr remaining bars)
//	Line 3: ↑ in · ↓ out · ⚡ cache  (session token totals + cache hits)
//
// Colors use the Okabe-Ito colorblind-safe palette. Fields gracefully omit
// when data is unavailable. Requires: bash, jq, awk, git (optional).
const sampleStatusScript = `#!/usr/bin/env bash
# ctm status line — consumed by claude's statusLine command feature.
# Three-line layout. Okabe-Ito colorblind-safe palette.
input=$(cat)

# Bar colors (line 2)
CYAN='\e[1;38;5;33m'     # Okabe-Ito blue (context)
MAGENTA='\e[1;38;5;220m'  # Okabe-Ito yellow/gold (weekly)
YELLOW='\e[1;38;5;208m'   # Okabe-Ito vermillion/orange (5-hour)
RESET='\e[0m'
# Header line colors
HDR_MODEL='\e[1;97m'
HDR_SEP='\e[90m'
HDR_PROJECT='\e[1;38;5;33m'  # match context bar

# ── Helper: build a 4-cell colored thin progress bar ─────────────────────────
make_bar() {
    local pct=$1
    local color=$2
    local bar_width=4
    local filled=$(( pct * bar_width / 100 ))
    [ "$filled" -gt "$bar_width" ] && filled=$bar_width
    local empty=$(( bar_width - filled ))
    local filled_str="" empty_str=""
    for (( i=0; i<filled; i++ )); do filled_str="${filled_str}━"; done
    for (( i=0; i<empty;  i++ )); do empty_str="${empty_str}─"; done
    printf '%b%s%b%s' "$color" "$filled_str" "$RESET" "$empty_str"
}

# ── Line 1: model + project ───────────────────────────────────────────────────
line1_parts=()

model=$(echo "$input" | jq -r '.model.display_name // .model.id // empty')
if [ -n "$model" ]; then
    short=$(echo "$model" | tr '[:upper:]' '[:lower:]' | grep -oE 'sonnet|opus|haiku|flash' | head -1)
    [ -z "$short" ] && short=$(echo "$model" | sed 's/ *([^)]*)//g' | tr '[:upper:]' '[:lower:]' | xargs)
    ctx_marker=$(echo "$model" | grep -oiE '[0-9]+[Mk]' | head -1)
    if [ -n "$ctx_marker" ]; then
        ctx_lower=$(echo "$ctx_marker" | tr '[:upper:]' '[:lower:]')
        skip=false
        case "$short" in
            sonnet|opus|haiku) [ "$ctx_lower" = "200k" ] && skip=true ;;
            flash)             [ "$ctx_lower" = "1m" ]   && skip=true ;;
        esac
        if [ "$skip" = false ]; then
            ctx_display=$(echo "$ctx_marker" | tr '[:lower:]' '[:upper:]')
            short="${short} (${ctx_display})"
        fi
    fi
    line1_parts+=("$short")
fi

project=$(echo "$input" | jq -r '.workspace.project_dir // .cwd // empty')
if [ -n "$project" ]; then
    if [[ "$project" == "$HOME" ]]; then
        short_project="~"
    elif [[ "$project" == "$HOME/"* ]]; then
        short_project="~${project#$HOME}"
    else
        short_project="$project"
    fi
    [ -n "$short_project" ] && line1_parts+=("$short_project")
fi

# ── Resolve git remote URL for project hyperlink ──────────────────────────────
project_url=""
if [ -n "$project" ]; then
    _is_repo=$(git -C "$project" rev-parse --is-inside-work-tree 2>/dev/null)
    if [ "$_is_repo" = "true" ]; then
        _remote=$(git -C "$project" remote get-url origin 2>/dev/null)
        if [ -n "$_remote" ]; then
            if [[ "$_remote" =~ ^git@([^:]+):(.+)$ ]]; then
                _host="${BASH_REMATCH[1]}"
                _path="${BASH_REMATCH[2]%.git}"
                project_url="https://${_host}/${_path}"
            elif [[ "$_remote" =~ ^https?:// ]]; then
                project_url="${_remote%.git}"
            fi
        fi
    fi
fi

sep="${HDR_SEP} · ${RESET}"

# ── Helper: format token count as k with 1 decimal or raw ────────────────────
fmt_tokens() {
    local n=$1
    if [ "$n" -ge 1000 ] 2>/dev/null; then
        awk -v n="$n" 'BEGIN { printf "%.1fk", n/1000 }'
    else
        echo "$n"
    fi
}

# ── Helper: wrap colored project segment with OSC 8 hyperlink if URL exists ──
render_project_seg() {
    local text="$1"
    local url="$2"
    local colored="${HDR_PROJECT}${text}${RESET}"
    if [ -n "$url" ]; then
        printf '\e]8;;%s\e\\%b\e]8;;\e\\' "$url" "$colored"
    else
        printf '%b' "$colored"
    fi
}

# ── Line 3: token segment (session cumulative + cache read from last call) ───
tok_in_raw=$(echo "$input" | jq -r '.context_window.total_input_tokens // empty')
tok_out_raw=$(echo "$input" | jq -r '.context_window.total_output_tokens // empty')
tok_cache_raw=$(echo "$input" | jq -r '.context_window.current_usage.cache_read_input_tokens // empty')

TOK_IN_COLOR='\e[1;38;5;33m'     # blue (match context)
TOK_OUT_COLOR='\e[1;38;5;37m'    # teal
TOK_CACHE_COLOR='\e[1;38;5;220m' # gold (match weekly)
DIM_GRAY='\e[90m'

tok_parts=()
[ -n "$tok_in_raw" ]    && [ "$tok_in_raw" -gt 0 ]    2>/dev/null && \
    tok_parts+=("$(printf '%b↑%b %b%s%b' "$TOK_IN_COLOR" "$RESET" "$DIM_GRAY" "$(fmt_tokens "$tok_in_raw")" "$RESET")")
[ -n "$tok_out_raw" ]   && [ "$tok_out_raw" -gt 0 ]   2>/dev/null && \
    tok_parts+=("$(printf '%b↓%b %b%s%b' "$TOK_OUT_COLOR" "$RESET" "$DIM_GRAY" "$(fmt_tokens "$tok_out_raw")" "$RESET")")
[ -n "$tok_cache_raw" ] && [ "$tok_cache_raw" -gt 0 ] 2>/dev/null && \
    tok_parts+=("$(printf '%b⚡%b %b%s%b' "$TOK_CACHE_COLOR" "$RESET" "$DIM_GRAY" "$(fmt_tokens "$tok_cache_raw")" "$RESET")")

token_seg=""
if [ "${#tok_parts[@]}" -gt 0 ]; then
    tok_joined="${tok_parts[0]}"
    for (( i=1; i<${#tok_parts[@]}; i++ )); do
        tok_joined="${tok_joined}  ${tok_parts[$i]}"
    done
    token_seg="$tok_joined"
fi

# ── Assemble line 1 ──────────────────────────────────────────────────────────
line1=""
if [ "${#line1_parts[@]}" -eq 1 ]; then
    line1="${HDR_MODEL}${line1_parts[0]}${RESET}"
elif [ "${#line1_parts[@]}" -ge 2 ]; then
    proj_seg=$(render_project_seg "${line1_parts[1]}" "$project_url")
    line1="${HDR_MODEL}${line1_parts[0]}${RESET}${sep}${proj_seg}"
fi

# ── Line 2: three labeled bars showing REMAINING capacity ────────────────────
bar_parts=()

ctx_raw=$(echo "$input" | jq -r '.context_window.used_percentage // empty')
if [ -n "$ctx_raw" ]; then
    pct=$(printf '%.0f' "$(echo "$ctx_raw" | awk '{print 100 - $1}')")
    bar_parts+=("$(printf '%b%s%b %s' "$CYAN" "c" "$RESET" "$(make_bar "$pct" "$CYAN")")")
fi

week_raw=$(echo "$input" | jq -r '.rate_limits.seven_day.used_percentage // empty')
if [ -n "$week_raw" ]; then
    pct=$(printf '%.0f' "$(echo "$week_raw" | awk '{print 100 - $1}')")
    bar_parts+=("$(printf '%b%s%b %s' "$MAGENTA" "w" "$RESET" "$(make_bar "$pct" "$MAGENTA")")")
fi

five_raw=$(echo "$input" | jq -r '.rate_limits.five_hour.used_percentage // empty')
if [ -n "$five_raw" ]; then
    pct=$(printf '%.0f' "$(echo "$five_raw" | awk '{print 100 - $1}')")
    bar_parts+=("$(printf '%b%s%b %s' "$YELLOW" "h" "$RESET" "$(make_bar "$pct" "$YELLOW")")")
fi

bar_line=""
for b in "${bar_parts[@]}"; do
    [ -z "$bar_line" ] && bar_line="$b" || bar_line="${bar_line}  $b"
done

# ── Output all lines ─────────────────────────────────────────────────────────
output=""
append() { [ -n "$output" ] && output="${output}"$'\n'"$1" || output="$1"; }
[ -n "$line1" ]     && append "$line1"
[ -n "$bar_line" ]  && append "$bar_line"
[ -n "$token_seg" ] && append "$token_seg"

[ -n "$output" ] && printf '%b\n' "$output"
`

// buildSampleOverlay returns the overlay JSON, pointing statusLine at the
// ctm-managed script and PostToolUse at the logging hook. Paths are resolved
// at write time so they match the user's actual config dir.
//
// Note: env vars like CLAUDE_CODE_NO_FLICKER cannot go here — claude reads
// them too early in startup for settings.json's env key to take effect.
// They live in ~/.config/ctm/env.sh (see sampleEnvFile) and are sourced
// by the shell before claude launches.
func buildSampleOverlay(scriptPath, logHookPath string) string {
	return fmt.Sprintf(`{
  "reduceMotion": false,
  "statusLine": {
    "type": "command",
    "command": %q
  },
  "theme": "dark",
  "hooks": {
    "PostToolUse": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": %q
          }
        ]
      }
    ]
  }
}
`, scriptPath, logHookPath)
}

// sampleEnvFile is the bash env script sourced by the tmux shell before
// claude launches. Use this for env vars that claude reads during CLI
// startup (e.g. CLAUDE_CODE_NO_FLICKER), which are too early for
// settings.json's env key to affect.
const sampleEnvFile = `# ctm-managed env file — sourced by the shell that spawns claude.
# Only affects claude processes launched via ctm. Direct 'claude' calls
# outside ctm are unaffected (this file is never sourced then).
#
# Add exports here for env vars claude reads early in startup.

# Flicker-free rendering for streaming markdown/code blocks (v2.1.88+).
export CLAUDE_CODE_NO_FLICKER=1

# Enable experimental Agent Teams feature.
export CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS=1
`

// writeEnvFile writes the default env.sh to path, creating parent dirs.
// Uses O_EXCL so parallel invocations don't clobber each other, and leaves
// an existing env file untouched (so user edits survive).
func writeEnvFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			return nil // keep user edits intact
		}
		return fmt.Errorf("creating env file: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(sampleEnvFile); err != nil {
		return fmt.Errorf("writing env file: %w", err)
	}
	return nil
}

// writeStatusScript writes the bash helper to path, creating parent dirs
// and marking it executable. Uses O_EXCL so parallel invocations can't
// clobber each other, and leaves an existing script untouched (so user
// edits survive).
func writeStatusScript(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0755)
	if err != nil {
		if os.IsExist(err) {
			return nil // keep user edits intact
		}
		return fmt.Errorf("creating status script: %w", err)
	}
	defer f.Close()
	if _, err := f.WriteString(sampleStatusScript); err != nil {
		return fmt.Errorf("writing status script: %w", err)
	}
	return nil
}

func runOverlayStatus(cmd *cobra.Command, args []string) error {
	out := output.Stdout()
	path := config.ClaudeOverlayPath()
	if _, err := os.Stat(path); err == nil {
		out.Success("overlay active: %s", path)
		scriptPath := statusScriptPath()
		if _, err := os.Stat(scriptPath); err == nil {
			out.Dim("status script: %s", scriptPath)
		}
		envPath := config.EnvFilePath()
		if _, err := os.Stat(envPath); err == nil {
			out.Dim("env file: %s", envPath)
		}
	} else {
		out.Dim("no overlay file at %s", path)
		out.Dim("create one with: ctm overlay init")
	}
	return nil
}

func runOverlayInit(cmd *cobra.Command, args []string) error {
	out := output.Stdout()
	path := config.ClaudeOverlayPath()
	scriptPath := statusScriptPath()
	envPath := config.EnvFilePath()
	hookCmd := logToolUseHookCommand()

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	// Write sidecar files (idempotent — keep existing).
	if err := writeStatusScript(scriptPath); err != nil {
		return err
	}
	if err := writeEnvFile(envPath); err != nil {
		return err
	}
	if err := os.MkdirAll(sessionLogDir(), 0755); err != nil {
		return fmt.Errorf("creating session log dir: %w", err)
	}

	// O_EXCL is atomic against concurrent creators — no TOCTOU race.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("overlay already exists at %s — delete it first or use `ctm overlay edit`", path)
		}
		return fmt.Errorf("creating overlay: %w", err)
	}
	defer f.Close()

	if _, err := f.WriteString(buildSampleOverlay(scriptPath, hookCmd)); err != nil {
		return fmt.Errorf("writing overlay: %w", err)
	}

	out.Success("created %s", path)
	out.Dim("status script: %s", scriptPath)
	out.Dim("env file: %s", envPath)
	out.Dim("PostToolUse hook: %s", hookCmd)
	out.Dim("session logs dir: %s (view: ctm logs)", sessionLogDir())
	out.Dim("edit with: ctm overlay edit")
	out.Dim("applies to all NEW ctm sessions; restart existing ones to pick up changes")
	return nil
}

func runOverlayEdit(cmd *cobra.Command, args []string) error {
	out := output.Stdout()
	path := config.ClaudeOverlayPath()
	scriptPath := statusScriptPath()
	envPath := config.EnvFilePath()
	hookCmd := logToolUseHookCommand()

	// Resolve editor BEFORE touching the filesystem so a missing $EDITOR
	// doesn't leave a half-created sample file behind.
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	editorBin, err := exec.LookPath(editor)
	if err != nil {
		return fmt.Errorf("editor %q not found in PATH — set $EDITOR or install it; overlay path: %s", editor, path)
	}

	// Create with sample if missing, atomically.
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return fmt.Errorf("creating config dir: %w", err)
		}
		if err := writeStatusScript(scriptPath); err != nil {
			return err
		}
		if err := writeEnvFile(envPath); err != nil {
			return err
		}
		_ = os.MkdirAll(sessionLogDir(), 0755)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
		if err != nil && !os.IsExist(err) {
			return fmt.Errorf("creating overlay: %w", err)
		}
		if err == nil {
			if _, werr := f.WriteString(buildSampleOverlay(scriptPath, hookCmd)); werr != nil {
				f.Close()
				return fmt.Errorf("writing overlay: %w", werr)
			}
			f.Close()
			out.Dim("created sample overlay at %s", path)
			out.Dim("status script: %s", scriptPath)
			out.Dim("env file: %s", envPath)
			out.Dim("PostToolUse hook: %s", hookCmd)
		}
	}

	c := exec.Command(editorBin, path)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}
