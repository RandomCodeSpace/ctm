# ctm — Claude Tmux Manager

Mobile-first session manager for [Claude Code](https://claude.com/claude-code) running in tmux. Survive SSH drops, reconnect from anywhere, one-tap resume, ctm-only claude customizations without touching your global config.

## Features

- **Persistent sessions.** tmux-backed. Claude keeps running when SSH drops; reattach from any device.
- **Mobile-friendly workflow.** `ctm last`, `ctm pick`, stale-session markers, Alt-a prefix alternative.
- **Tight lifecycle coupling.** When claude exits, the tmux session dies. No more stuck bash shells.
- **Crash-safe state.** Atomic writes, flock-based locking, corruption recovery on `sessions.json`.
- **Resume with fallback.** `claude --resume UUID || claude --session-id UUID` — recovers from missing session data.
- **Claude overlay.** Drop a `claude-overlay.json` to apply ctm-only settings (statusline, theme, etc.) without touching your global `~/.claude/settings.json`.
- **ctm-only shell env.** `~/.config/ctm/env.sh` is sourced before claude spawns — set env vars like `CLAUDE_CODE_NO_FLICKER=1` that claude reads during startup (too early for settings.json's `env` key).
- **Tool-use logging.** Built-in PostToolUse hook (pure Go, no jq/bash deps) writes one JSONL entry per tool call to `~/.config/ctm/logs/<session>.jsonl`. View with `ctm logs`.
- **YOLO mode.** Auto-commits a git checkpoint before bypassing permissions, so you can always roll back.
- **Preflight health checks.** Env vars, PATH, workdir, tmux session, claude process — cached for 60s to keep mobile reconnects snappy.
- **OSC52 clipboard sync.** Copy in tmux, paste anywhere.
- **Zero non-tmux runtime dependencies.** Pure Go throughout — native UUID, `/proc` walk, `filepath.WalkDir`. No `jq`, `pgrep`, `grep`, or `uuidgen` required.

## Installation

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

## Requirements

- Go 1.22+ (for `go install`)
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
| `ctm --verbose <cmd>` | Emit debug output for any command. |
| `ctm version` | Print version. |

### Claude overlay

| Command | Description |
|---|---|
| `ctm overlay` | Show overlay status (active / missing) with paths to sidecar files. |
| `ctm overlay init` | Create a sample `~/.config/ctm/claude-overlay.json` + `statusline.sh` + `env.sh` + hooks wiring. |
| `ctm overlay edit` | Open the overlay in `$EDITOR` (creates sidecars if missing). |
| `ctm overlay path` | Print the overlay file path. |

When the overlay file exists, ctm-spawned claude invocations get `--settings <path>` automatically, and `env.sh` is sourced by the shell before claude starts. Direct `claude` invocations outside ctm are untouched.

### Logs

| Command | Description |
|---|---|
| `ctm logs` | List sessions with tool-use logs, sorted by most recent. |
| `ctm logs <session-id>` | Dump a session's formatted tool-use log. |
| `ctm logs <session-id> -f` | Tail the log in real time. Handles rotation and truncation. |
| `ctm logs <session-id> --raw` | Print raw JSONL lines (pipe to `jq` for scripting). |

Logs are populated by a PostToolUse hook registered in the overlay. Each entry contains the full Claude Code hook payload plus a UTC timestamp. File perms 0600, session-id sanitized to prevent path traversal, concurrent writes coordinated via advisory flock.

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

Claude Code's TUI uses alt-screen and has no built-in scroll history. The app-intended workflow for scrollback on mobile is:

1. Enter tmux copy mode via **`Alt-[`** (or `Ctrl-b [`)
2. Swipe / arrow keys to scroll
3. `q` to exit

**Termius / WebSSH users:** Create a Snippet for one-tap access.
- Name: `scroll`
- Content: `<M-[>` (Alt-[)
- Assign an icon → tap the icon → instant copy mode.

## Configuration

- `~/.config/ctm/config.json` — main config (scrollback lines, required env vars, default mode, health check timeout, yolo checkpoint toggle)
- `~/.config/ctm/sessions.json` — session state (atomically written, flock-locked)
- `~/.config/ctm/tmux.conf` — generated tmux config (mobile-optimized, don't edit)
- `~/.config/ctm/claude-overlay.json` — optional claude settings overlay (statusline, theme, hooks) — created on first launch
- `~/.config/ctm/env.sh` — shell env sourced before claude spawns (for early-binding env vars like `CLAUDE_CODE_NO_FLICKER`)
- `~/.config/ctm/logs/<session-id>.jsonl` — per-session tool-use logs (0600)

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
