# Changelog

All notable changes to **ctm** are documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).
Each release is identified by an immutable `vX.Y.Z` git tag.

## How releases are produced

Releases are cut by the [`release.yml`](.github/workflows/release.yml)
workflow. On every push to `main` the workflow:

1. Runs the full Go test suite under the race detector
   (`go test -race ./...`).
2. Cross-compiles `linux-amd64`, `linux-arm64`, `darwin-amd64`,
   `darwin-arm64` binaries plus a vendored source tarball.
3. Publishes a GitHub Release with `SHA256SUMS`, conventional-commit
   grouped notes, and an air-gapped source archive.

This in-repo file is the canonical, human-curated history. The
matching GitHub Release page for each `vX.Y.Z` tag carries the
generated notes plus the signed checksums — see
<https://github.com/RandomCodeSpace/ctm/releases>.

## [Unreleased]

### Added

- Hermes agent support: `ctm new --agent hermes` and `ctm yolo --agent hermes`
  spawn a Hermes Agent (https://hermes-agent.dev) session with full resume +
  session-discovery parity to codex. `internal/agent/hermes/` mirrors
  `internal/agent/codex/` shape; resume is `hermes --resume <id>` (flag, not
  positional) and yolo flag is `--yolo`. Session ID discovery shells out to
  `hermes sessions list --source cli` rather than re-introducing a SQLite
  driver. Doctor dep list now walks `agent.Registered()` so future agents are
  picked up automatically without code edits.

### Changed

- `internal/config/config.go` schema bumped to v2. Existing
  `config.json` files with `required_in_path: ["claude", …]` are
  migrated to `["codex", …]` on next `ctm` invocation. User
  additions to the array are preserved.
- `.bestpractices.json` refreshed for the post-daemon, codex-only
  state: description swap, dropped `sqlite_fts5`/pnpm/playwright
  build references, dropped non-existent CodeQL evidence, flipped
  argon2id-related crypto criteria to N/A (auth subsystem removed),
  reframed dynamic-analysis justification around the post-spawn
  goroutine path. Maintainer still needs to click *Save (and
  continue) 🤖* on bestpractices.dev project 12716 to re-ingest.

### Fixed

- Codex thread UUID is now discovered and stamped onto
  `Session.AgentSessionID` post-spawn (5 s budget). Future reattach
  uses `codex resume <uuid>` instead of falling through to
  `codex resume --last`, so multi-session users land on the right
  thread.

## [0.3.0] — 2026-05-14

**BREAKING.** ctm is now a pure CLI. The embedded web dashboard and
the `ctm serve` HTTP daemon are gone entirely. Mobile-first SSH
reattach (`ctm last`, `ctm pick`, OSC52 clipboard, Alt-prefix keys)
remains the supported workflow.

### Removed

- The React web dashboard (`ui/` tree) and its build pipeline
  (`make ui`, `pnpm`, `vite`, `playwright`).
- The `ctm serve` HTTP daemon (`internal/serve/`) and its
  `ctm serve` subcommand.
- The `ctm auth` subcommand (single-user argon2id auth existed only
  to gate the now-removed web UI). The `~/.config/ctm/auth.json`
  password file is no longer read or written.
- All HTTP API endpoints (status feed, quota, session CRUD,
  remote-control bridge).
- The SSE event bus and live tool-use feed used by the dashboard.
- Outbound webhook delivery and webhook signing.
- `~/.config/ctm/allowed_origins` and the Origin / CORS allow-list —
  no HTTP surface remains to protect.
- The bearer-token + Origin gate documented in earlier SECURITY.md
  revisions.

### Changed

- `sessions.json` schema is unchanged from v3; existing sessions
  attach normally after upgrade.
- `release.yml` no longer builds the embedded UI; the release
  artifact set is unchanged (binaries + `SHA256SUMS` + vendored
  source tarball).
- SECURITY.md scope narrowed to the CLI, the on-disk state files,
  and supply-chain integrity. CONTRIBUTING.md drops UI / Playwright
  / `pnpm` instructions.

## [0.2.0] — 2026-05-14

**BREAKING.** Anthropic's `claude` CLI is no longer supported.
OpenAI's [`codex`](https://github.com/openai/codex) CLI (0.130.0+) is
now the only supported agent. Existing `sessions.json` rows are
migrated automatically on first launch.

### Changed

- `sessions.json` schema bumped to **v3**. The migrator rewrites any
  legacy `agent="claude"` rows to `agent="codex"` in place. The
  original bytes are preserved at `sessions.json.bak.<unix-nano>`
  per the standard migration safety net.
- Session resume now uses `codex resume <id> || codex` (with
  `codex resume --last` available for the most recent session)
  instead of `claude --resume … || claude --session-id …`.
- YOLO mode now launches `codex --sandbox danger-full-access`
  instead of `claude --dangerously-skip-permissions`. The git
  checkpoint behaviour is unchanged.
- README, SECURITY, and CONTRIBUTING rebranded: "Claude Tmux
  Manager" → "Codex Tmux Manager". The short name `ctm` is unchanged.

### Removed

- `claude-overlay.json` (the `--settings`-layered overlay file).
  Codex reads `~/.codex/config.toml` natively; ctm no longer
  maintains an overlay layer.
- `claude-env.json` / `env.sh` env-prelude. Codex's own
  `shell_environment_policy` covers this; users needing extra env
  should export from their own shell rc.
- `ctm overlay` and all its subcommands (`init` / `edit` / `path`).
- `ctm statusline` and the 3-line context-fill / rate-limit
  renderer. Codex doesn't emit equivalent telemetry, so the tmux
  statusline is now just session name + cwd + activity dot.
- `ctm logs` and the PostToolUse JSONL hook tailer. Codex's hook
  payload format differs and ctm no longer logs tool calls.
- `~/.claude/projects/*/<uuid>.jsonl` log tailer — no codex
  equivalent.

### Added

- `internal/agent/codex/` — the codex-specific Agent
  implementation (invocation, resume, sandbox flags). Replaces the
  previous claude-specific code path that lived under the same
  `Agent` interface.

## [0.1.0] — 2026-04-18 onwards

The `v0.1` line is the first stable series. Subsequent `0.1.x`
patches (v0.1.1 through v0.1.18 and ongoing) are non-breaking
hardening and coverage releases — see the GitHub Releases page for
per-patch notes. The line is summarised here by theme:

### Added

- OpenSSF Best Practices passing-tier wiring: `.bestpractices.json`,
  CI lint workflow, and the live badge in the README pointing at
  project [12716](https://www.bestpractices.dev/en/projects/12716).
  ([#17], [#18], [#19])
- OpenSSF Scorecard workflow on every push to `main` plus weekly
  schedule, results published at
  <https://scorecard.dev/viewer/?uri=github.com/RandomCodeSpace/ctm>.
  Badge wired in README. ([#16])
- `CONTRIBUTING.md` and `SECURITY.md` documenting PR conventions,
  bug-report flow, and the private vulnerability-reporting process.

### Changed

- Sonar maintainability and reliability passes: 256 → 0 outstanding
  smells. Mix of in-code fixes and explicit Accept / False Positive
  buckets via `.github/workflows/sonar-bulk-accept.yml`.
  ([#13], [#14], [#15])
- Test coverage uplifted past the 85% threshold across Go and
  TypeScript: UI Dashboard, hooks, `internal/serve` gaps,
  `cmd/yolo` refactor, `cmd/logs` and `cmd/overlay` extras,
  and SonarCloud's new-code coverage gate enforced on every PR.
  ([#10], [#11], [#12], [#13])
- CI runs `go test -race` on every PR and release; race-detector
  findings fail the build.

### Fixed

- Real data-race in test code (`cmd/logs_extra_test.go`) caught by
  `-race` in CI: `withFlags` helper's deferred restore raced the
  next test's read. Fixed by gating goroutine exit through
  `sync.WaitGroup`.

[#10]: https://github.com/RandomCodeSpace/ctm/pull/10
[#11]: https://github.com/RandomCodeSpace/ctm/pull/11
[#12]: https://github.com/RandomCodeSpace/ctm/pull/12
[#13]: https://github.com/RandomCodeSpace/ctm/pull/13
[#14]: https://github.com/RandomCodeSpace/ctm/pull/14
[#15]: https://github.com/RandomCodeSpace/ctm/pull/15
[#16]: https://github.com/RandomCodeSpace/ctm/pull/16
[#17]: https://github.com/RandomCodeSpace/ctm/pull/17
[#18]: https://github.com/RandomCodeSpace/ctm/pull/18
[#19]: https://github.com/RandomCodeSpace/ctm/pull/19

## [0.1.0] — 2026-04-18

First stable release. The CLI surface (`yolo`, `safe`, `attach`,
`kill`, `list`, `serve`) and the embedded `ctm serve` HTTP daemon
(V25 status feed, V26 quota tracking, V27 single-user auth via
argon2id + session tokens) are committed.

### Added

- Prebuilt cross-compiled binaries (`linux-amd64`, `linux-arm64`,
  `darwin-amd64`, `darwin-arm64`) and a vendored air-gapped source
  tarball published on every tag.
- `ctm serve` HTTP daemon binding `127.0.0.1` only by default,
  with mutation endpoints gated by bearer token + Origin allow-list.

### Changed

- README reshaped to promote prebuilt binaries in Quickstart;
  Requirements section trimmed.
- Release matrix dropped Windows targets — `syscall.Flock` is
  POSIX-only, and Windows users run the Linux binary under WSL.

## [0.0.1] — 2026-04 (and earlier)

Pre-stable releases. The `v0.0.x` line covered the initial
prototype (tmux session orchestration, Claude session bridging,
log capture). See the
[GitHub Releases page](https://github.com/RandomCodeSpace/ctm/releases)
for per-patch notes.
