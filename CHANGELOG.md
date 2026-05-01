# Changelog

All notable changes to **ctm** are documented in this file.

The format is based on [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning 2.0.0](https://semver.org/spec/v2.0.0.html).
Each release is identified by an immutable `vX.Y.Z` git tag.

## How releases are produced

Releases are cut by the [`release.yml`](.github/workflows/release.yml)
workflow. On every push to `main` the workflow:

1. Builds the embedded UI (`make ui`).
2. Runs the full Go test suite under the race detector
   (`go test -tags sqlite_fts5 -race ./...`).
3. Cross-compiles `linux-amd64`, `linux-arm64`, `darwin-amd64`,
   `darwin-arm64` binaries plus a vendored source tarball.
4. Publishes a GitHub Release with `SHA256SUMS`, conventional-commit
   grouped notes, and an air-gapped source archive.

This in-repo file is the canonical, human-curated history. The
matching GitHub Release page for each `vX.Y.Z` tag carries the
generated notes plus the signed checksums — see
<https://github.com/RandomCodeSpace/ctm/releases>.

## [Unreleased]

No unreleased changes.

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
