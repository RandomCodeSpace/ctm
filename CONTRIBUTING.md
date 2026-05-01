# Contributing to ctm

Thanks for considering a contribution. ctm is a single-maintainer
project today; PRs are welcome and reviewed on a best-effort basis.

## Reporting bugs or asking questions

- Open an issue: <https://github.com/RandomCodeSpace/ctm/issues>
- Search existing issues first; tag with the closest matching label.
- Include: ctm version (`ctm version`), OS, tmux version (`tmux -V`),
  shell, and the smallest reproducer you can produce.

For **security-relevant reports**, do **not** open a public issue —
follow the process in [SECURITY.md](./SECURITY.md).

## Pull requests

1. **Fork + branch from `main`.** Branch names use the conventional
   prefix that matches the change kind: `feat/...`, `fix/...`,
   `chore/...`, `docs/...`, `ci/...`, `refactor/...`, `test/...`.
2. **Keep PRs scoped.** One logical change per PR. Do not bundle
   refactoring or formatting passes with feature work.
3. **Tests are required for new logic.** SonarCloud's new-code
   coverage gate fails PRs that drop coverage below threshold. Match
   the existing test style in the package you're touching
   (table-driven Go tests, vitest + React Testing Library on the UI).
4. **All checks must pass before merge:** `go vet`, `go build`,
   `go test`, `pnpm exec tsc --noEmit`, `pnpm exec vitest run`,
   SonarCloud quality gate, CodeQL, and OpenSSF Scorecard. CI runs
   them automatically; locally you can run `make regression` for the
   superset.
5. **Conventional commit subjects.** Use the prefix that matches the
   change kind: `feat:`, `fix:`, `refactor:`, `chore:`, `docs:`,
   `test:`, `ci:`, `perf:`. The release workflow groups release notes
   by these prefixes (see `.github/workflows/release.yml`).
6. **Sign your commits if you can.** GitHub-verified signatures keep
   the OpenSSF Scorecard `Signed-Releases` and `Branch-Protection`
   checks healthy.

## Coding standards

| Area | Tool / convention |
|---|---|
| Go formatting | `gofmt -w` (run automatically by most editors) |
| Go vet | `go vet -tags sqlite_fts5 ./...` — must be clean |
| Go style | Standard Go review comments + Effective Go conventions |
| TypeScript | `pnpm exec tsc --noEmit` — strict mode is on; no `any` without justification |
| TS / React style | ESLint via the existing `ui/eslint.config.*` setup |
| Test layers | unit (pure logic), integration (DB + tmux), e2e (Playwright); see `rules/testing.md` in your local Claude config if you have one |
| File layout | Follow existing patterns. New `cmd/<x>.go` for cobra wiring + `cmd/<x>_runners.go` for integration-bound RunE bodies (see `cmd/yolo.go` ↔ `cmd/yolo_runners.go` for the split) |
| Comments | Lead with *why* a non-obvious decision was made. Don't restate what the code does |
| Dependency updates | Use `context7` MCP / vendor docs to find the latest compatible version; never guess. Permissive licenses only (MIT / Apache-2.0 / BSD) — flag GPL/AGPL for review |

## Static + dynamic analysis

Every PR is automatically:

- Statically analysed by **SonarCloud** (Go + TypeScript) and
  **CodeQL** (security families).
- Dynamically analysed by Go's **race detector** (`go test -race`)
  on every CI run. Data-race regressions fail the build.
- Audited by the **OpenSSF Scorecard** workflow on every push to
  `main`. The badge in the README reflects the live score.

If a static-analysis finding is a real bug, fix it. If it is a true
false positive or a deliberate accept, mark it via
`.github/workflows/sonar-bulk-accept.yml` (Sonar) or per-line
suppression with a justification comment.

## Vulnerability reports

See [SECURITY.md](./SECURITY.md). Don't file a public issue; use the
private security advisory link or email path documented there.

## License

By contributing you agree that your contribution will be licensed
under the [MIT License](./LICENSE) under which the project is
distributed.
