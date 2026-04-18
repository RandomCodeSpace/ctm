<h1 align="center">ctm</h1>

<p align="center"><i>Claude Tmux Manager — survive SSH drops, reattach from your phone.</i></p>

<p align="center">
  <a href="https://github.com/RandomCodeSpace/ctm/releases"><img src="https://img.shields.io/github/v/release/RandomCodeSpace/ctm?color=blue&include_prereleases&sort=semver" alt="Latest release"></a>
  <a href="https://goreportcard.com/report/github.com/RandomCodeSpace/ctm"><img src="https://goreportcard.com/badge/github.com/RandomCodeSpace/ctm" alt="Go Report Card"></a>
  <img src="https://img.shields.io/github/go-mod/go-version/RandomCodeSpace/ctm?color=00ADD8&label=go" alt="Go version">
  <a href="https://github.com/RandomCodeSpace/ctm/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="License MIT"></a>
</p>

<p align="center">
  <a href="#quickstart">Quickstart</a> ·
  <a href="#commands">Commands</a> ·
  <a href="#mobile-scroll">Mobile</a> ·
  <a href="#configuration">Config</a> ·
  <a href="#statusline">Statusline</a>
</p>

## Quickstart

```bash
go install github.com/RandomCodeSpace/ctm@latest
ctm                    # launches tmux + claude; drop SSH, reattach anytime
ctm last               # one-word reconnect from your phone
```

That's it. `ctm` bootstraps `~/.config/ctm/` on first run and injects shell aliases into `~/.bashrc` / `~/.zshrc` if they exist.

## Why ctm?

Claude Code on a remote dev box is great until your train enters a tunnel. Plain SSH + a direct `claude` invocation dies with the connection; reconnecting starts from scratch. **ctm wraps claude in tmux with mobile-first defaults** — Alt-based keybindings, OSC52 clipboard, one-keystroke session pickers, stale-session markers — so the conversation keeps running while you're underground, and reattaches from your phone with a single word.

```
Opus 4.7 (1M)  ~/projects/ctm
c 49% (486.8k)  w 34%  h 25%
↑ 118.6k  ↓ 434.8k  xhigh
```

(Above: the 3-line statusline ctm ships. Context fill + rate limits + cumulative tokens + current `/effort`, all from one hook.)

## Features

- **Mobile-first workflow.** `ctm last`, `ctm pick <filter>`, `Alt-a` second prefix, OSC52 clipboard sync, stale-session markers — the entire UX assumes you're on a phone with flaky Wi-Fi and a fat thumb.
- **Persistent sessions.** tmux-backed. Claude keeps running when SSH drops; reattach from any device.
- **Resume with fallback.** `claude --resume UUID || claude --session-id UUID` — recovers cleanly when session history is missing.
- **Tool-use logging.** Built-in PostToolUse hook writes one JSONL line per tool call; `ctm logs --since 7d --tool Bash --grep pattern` queries transparently across rotated history.
- **Claude overlay.** `claude-overlay.json` applies ctm-only settings (statusline, theme, hooks) without touching your global `~/.claude/settings.json`.
- **YOLO mode.** Auto-commits a git checkpoint before bypassing permissions, so you can always roll back.
- **Preflight health checks.** Env vars, PATH, workdir, tmux session, claude process — cached for 60 s to keep mobile reconnects snappy.
- **Tight lifecycle coupling.** When claude exits, the tmux session dies. No stuck bash shells, no zombie tabs.
- **Crash-safe state.** Atomic writes, flock-based locking, strict JSON decode with self-healing strip-to-.bak, `schema_version` + startup migrations on `sessions.json` / `config.json`.
- **Zero non-tmux runtime deps.** Pure Go throughout. No `jq`, `pgrep`, `grep`, or `uuidgen` required.

## Installation

### Prebuilt binary

Grab the archive for your platform from the [latest release](https://github.com/RandomCodeSpace/ctm/releases/latest), extract, and drop `ctm` into a directory on your `$PATH`:

| Platform | Asset |
|---|---|
| Linux x86_64 | `ctm-<version>-linux-amd64.tar.gz` |
| Linux ARM64 | `ctm-<version>-linux-arm64.tar.gz` |
| macOS (Intel) | `ctm-<version>-darwin-amd64.tar.gz` |
| macOS (Apple Silicon) | `ctm-<version>-darwin-arm64.tar.gz` |
| Windows x86_64 | `ctm-<version>-windows-amd64.zip` |

Every asset is accompanied by a `SHA256SUMS` file; verify with `sha256sum -c SHA256SUMS`.

### Version-pinned (recommended)

```bash
go install github.com/RandomCodeSpace/ctm@v0.1.0
```

Replace `v0.1.0` with any tag from the [releases page](https://github.com/RandomCodeSpace/ctm/releases). Every release includes the exact `go install` command for that version in its release notes.

### Latest from main

```bash
go install github.com/RandomCodeSpace/ctm@latest
```

### Post-install

No extra setup step is required — the first time you run any claude-launching command (`ctm`, `ctm <name>`, `ctm new`, `ctm yolo`), ctm bootstraps `~/.config/ctm/` with sensible defaults, regenerates `tmux.conf` on every launch, and injects shell aliases into `~/.bashrc` / `~/.zshrc` if they exist.

If you prefer an explicit setup step (or want the cc-session migration to run), `ctm install` still does the same work upfront.

<details>
<summary><b>Lifecycle hooks</b> — fire shell commands on <code>on_attach</code> / <code>on_new</code> / <code>on_yolo</code> / <code>on_safe</code> / <code>on_kill</code></summary>

&nbsp;

ctm can fire a user-supplied shell command on five lifecycle events. Declare them under `hooks` in `~/.config/ctm/config.json`:

```json
{
  "hooks": {
    "on_attach": "notify-send 'attached to $CTM_SESSION_NAME'",
    "on_new":    "echo \"$(date -Iseconds) new $CTM_SESSION_NAME\" >> ~/.ctm-audit.log",
    "on_yolo":   "curl -s -X POST https://hooks.example/ctm/yolo -d name=$CTM_SESSION_NAME",
    "on_safe":   "echo safe $CTM_SESSION_NAME",
    "on_kill":   "tar czf ~/session-snapshots/$CTM_SESSION_NAME-$(date +%s).tgz -C $CTM_SESSION_WORKDIR ."
  },
  "hook_timeout_seconds": 5
}
```

Each command runs through `sh -c` with the following env vars:

| Var | Value |
|---|---|
| `CTM_EVENT` | event name (e.g. `on_attach`) |
| `CTM_SESSION_NAME` | session name |
| `CTM_SESSION_UUID` | session UUID |
| `CTM_SESSION_MODE` | `safe` or `yolo` |
| `CTM_SESSION_WORKDIR` | absolute workdir |

Hooks run **synchronously** with a per-hook wall-clock ceiling (default 5 s, override via `hook_timeout_seconds`). Failures log a WARN-level slog line and are otherwise ignored — they never block the action that triggered them. For fire-and-forget semantics, append `&` inside the shell command.

</details>

<details>
<summary><b>Shell completion</b> — bash / zsh / fish / powershell</summary>

&nbsp;

`ctm completion [bash|zsh|fish|powershell]` emits a completion script on stdout. Install per shell:

```bash
# bash — system-wide
ctm completion bash | sudo tee /etc/bash_completion.d/ctm >/dev/null

# bash — per-user (append to ~/.bashrc)
echo 'source <(ctm completion bash)' >> ~/.bashrc

# zsh — assumes fpath is set and there is a completion dir in it
ctm completion zsh > "${fpath[1]}/_ctm"

# fish
ctm completion fish > ~/.config/fish/completions/ctm.fish

# powershell — session only
ctm completion powershell | Out-String | Invoke-Expression
```

Completion is aware of subcommands, flags, and (for `ctm attach`, `ctm kill`, `ctm rename`, etc.) live session names pulled from `~/.config/ctm/sessions.json`.

</details>

## Requirements

- Go 1.25+ (for `go install`)
- tmux 3.0+
- [Claude Code CLI](https://claude.com/claude-code) on `$PATH`
- A terminal that speaks xterm + OSC52 (Termius, WebSSH, iTerm2, Kitty, wezterm, Windows Terminal)

## Commands

### Attach / create

| Command | Description |
|---|---|
| `ctm` | Attach to the default session (`claude`). Creates it if missing. |
| `ctm <name>` | Attach to a named session, or create it. |
| `ctm cc` | Shorthand for attaching to `cc`. |
| `ctm new <name>` | Create a new session in a specific workdir. |
| `ctm yolo [name]` | Create/attach a YOLO session (permissions bypassed + git checkpoint). |

### Navigation

| Command | Description |
|---|---|
| `ctm last` (alias `l`) | Attach to the most recently used LIVE session. Mobile reconnect in one word. |
| `ctm pick [filter]` (alias `p`) | Interactive session picker. With filter, narrows to substring match; single match auto-attaches. Inside tmux uses the native `choose-session`. |
| `ctm switch <name>` (alias `sw`) | Switch to a named session (uses `switch-client` inside tmux). |
| `ctm ls` (alias `list`) | List all sessions with mode, live status, age, idle time, and `[STALE]` markers for sessions idle > 7 days. |

### Lifecycle

| Command | Description |
|---|---|
| `ctm detach` | Detach the current tmux client. Same as `Alt-d` inside a session. |
| `ctm kill <name>` | Kill a tmux session and its claude process. |
| `ctm forget <name>` | Remove a session from the store without killing tmux. |
| `ctm rename <old> <new>` | Rename a session across ctm state and tmux. |

### Diagnostics

| Command | Description |
|---|---|
| `ctm check` | Run preflight health checks (exits non-zero on failure). |
| `ctm doctor` | Show detailed environment, session state, and suggested fixes. |
| `ctm --verbose <cmd>` | Emit debug output for any command (alias for `--log-level=debug`). |
| `ctm --log-level <lvl>` | Structured diagnostic log level on stderr: `debug`\|`info`\|`warn`\|`error`. Default: `info`. Set `CTM_LOG_FORMAT=json` for NDJSON output. |
| `ctm version` | Print version. |

### Claude overlay

| Command | Description |
|---|---|
| `ctm overlay` | Show overlay status (active / missing) with paths to sidecar files. |
| `ctm overlay init` | Create a sample `~/.config/ctm/claude-overlay.json` + `statusline.sh` + `env.sh` + hooks wiring. |
| `ctm overlay edit` | Open the overlay in `$EDITOR` (creates sidecars if missing). |
| `ctm overlay path` | Print the overlay file path. |

When the overlay file exists, ctm-spawned claude invocations get `--settings <path>` automatically, and `env.sh` is sourced by the shell before claude starts. Direct `claude` invocations outside ctm are untouched.

#### Statusline

ctm ships a 3-line statusLine renderer (`ctm statusline`) that the overlay wires into Claude Code as `statusLine.command`. Layout:

```
Opus 4.7 (1M)  ~/projects/ctm
c 49% (486.8k)  w 34%  h 25%
↑ 118.6k  ↓ 434.8k  xhigh
```

- **Line 1** — model name (redundant `Claude` / `claude-` prefix stripped) and project dir (OSC 8 hyperlinked to the `origin` remote when a `.git/config` is found).
- **Line 2** — `c` context used + tokens currently consumed (input-only sum per Claude Code's formula), `w` weekly rate-limit usage, `h` 5-hour rate-limit usage. Percentages taken verbatim from the payload; parenthesised token count formatted with SI suffix (`k` / `M` / `B`).
- **Line 3** — `↑` cumulative session input tokens, `↓` cumulative session output tokens (SI-formatted). Current `/effort` level is appended dim-gray (`min`/`low`/`medium`/`high`/`xhigh`/`max`), sourced from `~/.claude/settings.json` since Claude Code's statusLine payload does not expose it.

Sections with missing payload fields are silently skipped, and at the default `INFO` log level nothing is written to stderr. To wire it into Claude Code outside a ctm-spawned session too, set `statusLine` in `~/.claude/settings.json`:

```json
{
  "statusLine": { "type": "command", "command": "ctm statusline" }
}
```

### Logs

| Command | Description |
|---|---|
| `ctm logs` | List sessions with tool-use logs, sorted by most recent. |
| `ctm logs <session-id>` | Dump a session's formatted tool-use log. |
| `ctm logs <session-id> -f` | Tail the log in real time. Handles rotation and truncation. |
| `ctm logs <session-id> --raw` | Print raw JSONL lines (pipe to `jq` for scripting). |
| `ctm logs <session-id> --since 7d` | Only show entries newer than this duration (`Nd` shorthand for days). |
| `ctm logs <session-id> --tool Bash` | Only show entries whose `tool_name` matches (case-insensitive). |
| `ctm logs <session-id> --grep '\bpassword\b'` | Only show entries whose raw JSON matches this regex. |

Filters AND together and apply across both the active log and rotated `.gz` siblings.

Logs are populated by a PostToolUse hook registered in the overlay. Each entry contains the full Claude Code hook payload plus a UTC timestamp. File perms 0600, session-id sanitized to prevent path traversal, concurrent writes coordinated via advisory flock.

**Rotation & retention.** When a session's active log crosses the size cap (default 50 MiB), it is renamed to `<session>.jsonl.<unix-nano>`, gzipped in place to `<session>.jsonl.<unix-nano>.gz`, and a fresh empty active log replaces it. Rotated siblings are pruned beyond the age cap (default 30 days) or count cap (default 10 files). `ctm logs <session>` and `ctm logs <session> -f` read the active log **and** every rotated `.gz` sibling transparently, so history spanning rotations is a single chronological stream. Override the defaults in `config.json`:

```json
{
  "log_max_size_mb": 100,
  "log_max_age_days": 14,
  "log_max_files": 5
}
```

A zero value means "use the built-in default" — to effectively disable a cap, set it to a very large number.

## Keybindings

Inside any ctm tmux session:

| Key | Action |
|---|---|
| `Ctrl-b` | Default tmux prefix |
| `Alt-a` | Mobile-friendly second prefix |
| `Alt-[` | Enter copy mode (no prefix needed) |
| `Alt-d` | Detach client |
| `Ctrl-b [` | Enter copy mode (standard tmux) |

## Mobile scroll

> **The mobile scrollback trick.** Claude Code's TUI uses alt-screen and has no built-in scroll history. To scroll back on a phone:
>
> 1. Press **`Alt-[`** (or `Ctrl-b [`) — enters tmux copy mode.
> 2. Swipe / arrow keys to scroll.
> 3. `q` to exit.

**Termius / WebSSH users:** Wire Alt-[ to a one-tap icon with a Snippet.

| Field | Value |
|---|---|
| Name | `scroll` |
| Content | `<M-[>` (Alt-[) |
| Assign icon | any — tap it for instant copy mode |

## Configuration

- `~/.config/ctm/config.json` — main config (scrollback lines, required env vars, default mode, health check timeout, yolo checkpoint toggle)
- `~/.config/ctm/sessions.json` — session state (atomically written, flock-locked)
- `~/.config/ctm/tmux.conf` — generated tmux config (mobile-optimized, don't edit)
- `~/.config/ctm/claude-overlay.json` — optional claude settings overlay (statusline, theme, hooks) — created on first launch
- `~/.config/ctm/env.sh` — shell env sourced before claude spawns (for early-binding env vars like `CLAUDE_CODE_NO_FLICKER`)
- `~/.config/ctm/logs/<session-id>.jsonl` — per-session tool-use logs (0600)

### State file versioning

`config.json` and `sessions.json` carry a top-level `"schema_version"` integer. On every startup ctm runs a migration pass: if a file is below the current schema version, the migrator applies pending steps, stamps the new version, and writes atomically. **Before any destructive migration write, the original bytes are copied to `<path>.bak.<unix-nano>`** — recovery is always one `mv` away. A newer-than-known `schema_version` causes a hard refusal to start rather than silent downgrade. Missing files are left untouched; the migrator never creates them.

## Upgrading

```bash
go install github.com/RandomCodeSpace/ctm@latest
```

Then regenerate the tmux config to pick up any new defaults:

```bash
rm ~/.config/ctm/tmux.conf
ctm cc
```

## License

MIT
