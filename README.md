<h1 align="center">ctm</h1>

<p align="center"><i>Codex Tmux Manager — survive SSH drops, reattach from your phone.</i></p>

<p align="center">
  <a href="https://github.com/RandomCodeSpace/ctm/releases"><img src="https://img.shields.io/github/v/release/RandomCodeSpace/ctm?color=blue&include_prereleases&sort=semver" alt="Latest release"></a>
  <a href="https://goreportcard.com/report/github.com/RandomCodeSpace/ctm"><img src="https://goreportcard.com/badge/github.com/RandomCodeSpace/ctm" alt="Go Report Card"></a>
  <img src="https://img.shields.io/github/go-mod/go-version/RandomCodeSpace/ctm?color=00ADD8&label=go" alt="Go version">
  <a href="https://github.com/RandomCodeSpace/ctm/blob/main/LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="License MIT"></a>
  <a href="https://scorecard.dev/viewer/?uri=github.com/RandomCodeSpace/ctm"><img src="https://api.scorecard.dev/projects/github.com/RandomCodeSpace/ctm/badge" alt="OpenSSF Scorecard"></a>
  <a href="https://www.bestpractices.dev/projects/12716"><img src="https://www.bestpractices.dev/projects/12716/badge" alt="OpenSSF Best Practices"></a>
</p>

<p align="center">
  <a href="#quickstart">Quickstart</a> ·
  <a href="#commands">Commands</a> ·
  <a href="#mobile-scroll">Mobile</a> ·
  <a href="#configuration">Config</a>
</p>

## Quickstart

Download the prebuilt binary for your platform (no Go toolchain needed):

```bash
# Linux x86_64 — adjust URL for your OS/arch (see below).
curl -LO https://github.com/RandomCodeSpace/ctm/releases/latest/download/ctm-$(curl -s https://api.github.com/repos/RandomCodeSpace/ctm/releases/latest | jq -r .tag_name)-linux-amd64.tar.gz
tar xzf ctm-*-linux-amd64.tar.gz && sudo mv ctm-*/ctm /usr/local/bin/

ctm                    # launches tmux + codex; drop SSH, reattach anytime
ctm last               # one-word reconnect from your phone
```

Or with Go installed:

```bash
go install github.com/RandomCodeSpace/ctm@latest
```

Either way, `ctm` bootstraps `~/.config/ctm/` on first run and injects shell aliases into `~/.bashrc` / `~/.zshrc` if they exist.

## Why ctm?

Codex on a remote dev box is great until your train enters a tunnel. Plain SSH + a direct `codex` invocation dies with the connection; reconnecting starts from scratch. **ctm wraps codex in tmux with mobile-first defaults** — Alt-based keybindings, OSC52 clipboard, one-keystroke session pickers, stale-session markers — so the conversation keeps running while you're underground, and reattaches from your phone with a single word.

```
codex 0.130.0  ~/projects/ctm  ●
```

(Above: ctm's tmux statusline — agent + cwd + activity dot. Codex doesn't expose context/rate-limit telemetry, so the line stays minimal.)

## Features

- **Mobile-first workflow.** `ctm last`, `ctm pick <filter>`, `Alt-a` second prefix, OSC52 clipboard sync, stale-session markers — the entire UX assumes you're on a phone with flaky Wi-Fi and a fat thumb.
- **Persistent sessions.** tmux-backed. Codex keeps running when SSH drops; reattach from any device.
- **Resume with fallback.** `codex resume <id> || codex` — recovers cleanly when the prior session can't be re-opened. Use `codex resume --last` for the most recent.
- **YOLO mode.** Auto-commits a git checkpoint before launching with `codex --sandbox danger-full-access`, so you can always roll back.
- **Preflight health checks.** Env vars, PATH, workdir, tmux session, codex process — cached for 60 s to keep mobile reconnects snappy.
- **Tight lifecycle coupling.** When codex exits, the tmux session dies. No stuck bash shells, no zombie tabs.
- **Multi-agent.** Codex is the default; pass `--agent hermes` to `ctm new` or `ctm yolo` to spawn [Hermes Agent](https://hermes-agent.dev) instead. New agents plug in via `internal/agent.Register` without touching call sites.
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

Every asset is accompanied by a `SHA256SUMS` file; verify with `sha256sum -c SHA256SUMS`. Windows users: run the Linux binary under WSL — tmux has no native Windows support.

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

No extra setup step is required — the first time you run any codex-launching command (`ctm`, `ctm <name>`, `ctm new`, `ctm yolo`), ctm bootstraps `~/.config/ctm/` with sensible defaults, regenerates `tmux.conf` on every launch, and injects shell aliases into `~/.bashrc` / `~/.zshrc` if they exist.

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

- tmux 3.0+
- [Codex CLI](https://github.com/openai/codex) on `$PATH` (install via `npm i -g @openai/codex` or your package manager of choice)
- A terminal that speaks xterm + OSC52 (Termius, WebSSH, iTerm2, Kitty, wezterm, Windows Terminal)
- Go 1.25+ — **only** if you build from source (`go install`); prebuilt binaries have no Go dependency
- Linux or macOS — Windows is not supported natively; use WSL

## Commands

### Attach / create

| Command | Description |
|---|---|
| `ctm` | Attach to the default session (`codex`). Creates it if missing. |
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
| `ctm kill <name>` | Kill a tmux session and its codex process. |
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

> **The mobile scrollback trick.** Codex's TUI uses alt-screen and has no built-in scroll history. To scroll back on a phone:
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
