# ctm Robustness Audit — 2026-04-18

Scope: `/home/dev/projects/ctm`, branch `main`, HEAD `0ed4e4a`.
Method: read-only audit across 5 parallel subagents. No code changed.
Deployment context: single-user dev CLI, Linux-primary, mobile-first (phone → laptop reconnect), air-gapped-deployable.

## 1. Status Table

| # | Item | Status | Location (primary) | Concrete gap |
|---|------|--------|---------------------|--------------|
| 1 | Integration tests: SSH drop/reattach, concurrent ctm, tmux killed mid-op, truncated JSONL, claude non-zero exit | partial | `integration_test.go:28-138` | Only basic create/kill/install paths covered. None of the 5 failure scenarios are simulated. |
| 2 | Property-based tests for sessions.json + flock | no | — | No gopter/rapid/`testing/quick`. Flock semantics never exercised under contention. |
| 3 | Cross-platform `/proc` abstraction | no | `internal/claude/process.go:26,59,74` | Linux-only. Hard-coded `/proc`. No `runtime.GOOS` branches. `FindClaudeChild` silently fails on macOS/BSD. |
| 4 | Schema validation for `claude-overlay.json` | partial | `cmd/overlay.go` (loader); `internal/config/config.go:96-103` | Typed struct but no `DisallowUnknownFields`. User typos silently swallowed. Strictness is a potential breaking change — see §5. |
| 5 | `env.sh` sourcing sandbox | mostly ok | `internal/claude/command.go:95-98` | Already hardened: shell-quoted, `[ -r path ]` guard, `\|\| true` wrap. No signature check — acceptable for user-owned path. |
| 6 | File perms audit | partial | `cmd/overlay.go:134` (0644), `internal/session/state.go` (mixed) | `claude-overlay.json` and `sessions.json` backup files created 0644 — should be 0600. Lock file 0600 ✓. Log dir 0700 ✓. |
| 7 | YOLO checkpoint tamper-evidence | no | `cmd/yolo.go:257-271` | Plain unsigned `git commit`. No signed tag, no GPG, no SHA pin. |
| 8 | OSC52 escape guards | n/a | `internal/tmux/config.go:1-47` | ctm does not emit OSC52 directly. Only sets `tmux set -g set-clipboard on`. Nothing to guard. |
| 9 | Release pipeline: cosign / SBOM / checksums | no | `.github/workflows/release.yml` | GitHub release only, unsigned binaries, no SBOM, no checksum file. |
| 10 | Structured logging (`log/slog`) + `--log-level` | no | `internal/output/printer.go` | All logging via `fmt.Print*` + `output.Printer`. Only boolean `--verbose/-v`. |
| 11 | OpenTelemetry spans + `TRACEPARENT` propagation | no | — | No `go.opentelemetry.io/*` imports. No span instrumentation. |
| 12 | Counters / `ctm stats` / Prometheus textfile | no | — | No metrics surface. |
| 13 | JSONL rotation + retention | no | `cmd/log_tool_use.go` | Unbounded growth on `~/.config/ctm/logs/<session>.jsonl`. No size/age caps. Rotated path layout is a potential breaking change for tooling that reads the raw path — see §5. |
| 14 | `ctm logs` query primitives | partial | `cmd/logs.go` | Only `--follow`, `--raw`. No `--since`, `--tool`, `--grep`. |
| 15 | Streaming socket (Unix/WS NDJSON) | no | — | No server of any kind. Port 37777 is Claude Code, not ctm. |
| 16 | `schema_version` field in state files | no | `internal/config/config.go`, `internal/session/state.go` | No version marker. De facto v1 by convention. |
| 17 | Startup migration runner | partial | `cmd/install.go` | Migration runs only on `ctm install`. `Backup()` exists but auto-called only on `DeleteAll`. |
| 18 | Shell completion (`ctm completion …`) | partial | `cmd/root.go:45` | Cobra wired but `completion` subcommand not registered. No README install path. |
| 19 | Lifecycle hooks (`on_attach`, `on_yolo`, `on_kill`) | no | `internal/config/config.go` | No hook fields, no dispatcher. PostToolUse exists but hardcoded to log writer. |
| 20 | Git remote session-state sync | no | `cmd/yolo.go` | `gitCheckpoint()` is local-only (`add`, `commit`). No push/pull. |

## 2. Severity Ranking

Scored on likelihood × blast radius **for ctm's actual audience** — single dev, mobile-first, local tool. Novelty discounted.

### Critical
_None._ ctm is not exposed to untrusted input. Single-user local tool. No CVSS-9 surface.

### High
- **#13 — JSONL rotation/retention.** Unbounded growth. Every heavy Claude Code user will hit this. Silent disk bloat on the mobile-first device (phone/tablet) is a real hazard; no operator watching.
- **#16 + #17 — `schema_version` + migration runner.** Cheapest to add now, exponentially expensive after state diverges across users. Paired unit.
- **#1 — Integration test gap (subset).** Specifically: concurrent ctm on `sessions.json`, and truncated JSONL on read. These map directly to the mobile-first reconnect story. Phone + laptop racing to `sessions.json` is not hypothetical.
- **#10 — `log/slog` + `--log-level`.** Prerequisite for diagnosing issues without strace/printf surgery. Low cost (stdlib only, Go 1.22+). Shrinks future blast radius on every other bug.

### Medium
- **#6 — File perms tighten.** Flip `claude-overlay.json` and sessions backup files to 0600. Trivial diff, single commit, removes a paper-cut.
- **#4 — `DisallowUnknownFields`.** User edits overlay by hand; current behaviour silently drops typos. UX win as much as security.
- **#18 — Shell completion subcommand.** Cobra generates it for free. ~30 LOC + a README block.
- **#2 — Property-based tests (rapid).** Pure-Go dep, no new heavyweight framework. Targeted at the flock/session state machine only.
- **#14 — `ctm logs --since / --tool` filters.** Genuinely useful for the mobile debug loop.
- **#19 — Lifecycle hooks.** User-extensible, but small defined surface (on_attach, on_yolo, on_kill). Must resist scope creep.

### Low
- **#1 subset — SSH drop/reattach simulation, claude non-zero exit, tmux killed mid-op.** Valuable but harder to simulate deterministically. Partial coverage via existing integration harness may be enough; further expansion has diminishing returns.
- **#9 (partial) — checksums in release workflow.** Cheap (`sha256sum` + upload). Adopt checksums only; defer cosign/SBOM.
- **#11 — OpenTelemetry.** Over-engineered for a single-user CLI. Air-gapped constraint means no OTel backend anyway.
- **#12 — metrics surface.** No consumer exists.
- **#15 — streaming socket.** Speculative. No current subscriber to justify the protocol decision.

## 3. Recommended Drops / Defers

Pushing back on the original list where it does not fit this codebase:

| # | Recommendation | Reason |
|---|---------------|--------|
| 3 | **Defer** cross-platform `/proc` | Current user base is Linux. Accept as known limitation, document in README, re-open when first Mac user files an issue. Cheap to bolt on later (`process_linux.go` / `process_darwin.go` build-tag split). |
| 7 | **Drop** signed YOLO checkpoints | The checkpoint lives in the developer's own local repo on their own machine. Signing it is theatre — if an attacker can write to your workdir, they can also write signing material. Plain commit is correct. |
| 8 | **Drop** OSC52 guards | Not applicable — ctm does not emit OSC52. |
| 9 | **Partial-adopt** release pipeline | Take `sha256sum` checksums (cheap, 5 LOC in workflow). **Defer** cosign/SBOM — build-from-source policy in `rules/build.md` says binaries are user-produced, not a shipping medium. |
| 11 | **Drop** OpenTelemetry | Single-user local CLI, no tracing backend in the intended deploy envelope. Adds runtime weight without a reader. Revisit only if ctm grows a server component. |
| 12 | **Drop** Prometheus/metrics surface | No consumer. JSONL logs already carry enough for offline analysis. |
| 15 | **Defer** streaming socket | Build the first consumer (e.g. a web dashboard) first; the socket protocol should be shaped by its caller, not guessed now. |
| 20 | **Drop** git-remote multi-host sync | Conflict-resolution semantics on `sessions.json` across hosts is a product question, not a feature. User can already `git`-version `~/.config/ctm/` manually if they want. |

## 4. Recommended Implementation Order

Logical dependency chain, quick wins first, no breaking churn until migration runner is in place.

### Phase 3A — Foundation (land before anything else touches state)
1. **#16 + #17 — `schema_version` + startup migration runner.** One commit per file touched, but conceptually one unit. Adds `"schema_version": 1` to `config.json`, `sessions.json`, `claude-overlay.json`. Startup-time migrator with `.bak` backup before any destructive write. Unblocks every subsequent state change.
2. **#6 — File perms 0600.** Trivial, atomic, single commit.
3. **#13 — JSONL rotation + retention.** Size cap (e.g. 50 MiB) + age cap (e.g. 30 days), both configurable via `config.json` with sensible defaults. Runs on startup and after each append. **Breaking-change mitigation:** rotated files live alongside the active log (`<session>.jsonl.1.gz`, `.2.gz` …). `ctm logs` (and the #14 filters) must transparently read across the active + rotated set so the supported query path never regresses. Document the rotated-file names in the README.

### Phase 3B — Observability + DX
4. **#10 — `log/slog` + `--log-level`.** Introduce a `internal/logging` package. Root cobra flag. Keep `output.Printer` for user-facing stdout; slog for diagnostics. No behaviour change at default level.
5. **#4 — `DisallowUnknownFields` on all JSON loaders.** Wrap in a shared helper (sister to `internal/claude/jsonpatch.go`). **Breaking-change mitigation:** on first load under v1, do not hard-fail on unknown keys. Instead, strip unknowns into a sibling `.bak` (preserving the original bytes) and emit a WARN-level slog line naming each dropped key. After the first clean load, the helper enforces strict decoding. This lets existing hand-edited overlays upgrade once without losing data, while typos on future edits get caught immediately. Requires #10 slog to be in place first — hence the ordering.
6. **#18 — `ctm completion {bash,zsh,fish}` subcommand.** Cobra native.

### Phase 3C — Tests
7. **#2 — Property-based tests (rapid).** Target: sessions.json state machine invariants, flock mutual-exclusion under N writers.
8. **#1 (subset) — Integration tests.** Concurrent `ctm` processes, truncated JSONL on read. Use `t.TempDir()` + real tmux + goroutine fan-out.

### Phase 3D — Features (only with explicit approval)
9. **#14 — `ctm logs --since / --tool / --grep`.** Reads existing JSONL, streams filtered output.
10. **#19 — Lifecycle hooks.** Narrow surface, shell-exec in a goroutine with timeout + `TMUX_SESSION` etc. env, config-driven.
11. **#9 (partial) — checksums in release workflow.** `sha256sum *.tar.gz > SHA256SUMS` + upload as release asset.

## 5. Breaking Changes & Mitigations

Two items in the Phase 3 plan change observable behaviour. Both have planned mitigations; everything else is purely additive.

| # | Surface | Breakage | Mitigation |
|---|---------|----------|------------|
| 4 | `claude-overlay.json`, `config.json` strict loading | Hand-edited files with typos / legacy keys would start failing to load under strict decode. | On first v1 load: strip unknowns into a sibling `.bak`, WARN via slog, continue. Strictness enforced only on subsequent edits. Requires #10 first. |
| 13 | JSONL log path layout | Active log stays at `<session>.jsonl`; history moves to `<session>.jsonl.1.gz`, `.2.gz` …. External `cat`/`tail`/`grep` on the raw path loses access to rotated history. | `ctm logs` + #14 filters must read active + rotated transparently so the supported path doesn't regress. Keep generous defaults (30 d, 50 MiB). Document rotated file names in README. |

Items **not** breaking despite superficial resemblance:

- **#16 / #17 schema_version + migrator.** Backward-compat only. Missing field → treat as v0 → migrate to v1 with `.bak`. Older ctm reading a newer file survives because the current loader ignores unknown keys (and after #4 lands, the stripping path handles it too).
- **#6 perms 0600.** Tightening only; same-uid caller unaffected.
- **#10 slog.** Default level keeps stdout quiet; diagnostics go to stderr and are opt-in via `--log-level`.
- **#18 completion, #14 logs filters, #19 hooks, #9 checksums.** Pure additions.

## 6. Cross-Cutting Notes (not in original list)

Surfaced during audit — filed here, not silently fixed per `rules/git.md`:

- `internal/session/state.go` uses flock across all Store ops (good), but `load()`→`save()` is not atomically bracketed under a single lock in callers. A rare interleave on concurrent writers could cost an update. Worth verifying when writing the property test in Phase 3B.
- `sessions.json` corruption recovery writes `.corrupt.<nanotime>` and returns empty state. After recovery the next write silently overwrites any session info not re-registered. Worth a WARN-level slog line once slog lands (#10).
- `integration_test.go` has no `t.Parallel()` anywhere. Safe default, but if future property tests want parallelism they'll need a separate suite with its own `TMUX_TMPDIR`.

---

**END PHASE 2.** No code has been modified in this pass. Awaiting approval on:

1. The severity ranking.
2. The proposed drops/defers (especially #3, #7, #11, #12, #15, #20).
3. The Phase 3 implementation order, or a reordering.

Once you approve (or amend), I will begin Phase 3A in a fresh test-first commit per item.
