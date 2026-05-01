# Security Policy

## Supported versions

ctm follows semantic versioning. Security fixes are issued only for
the latest released minor version. There is no long-term-support
branch — older releases do not receive backports.

| Version | Supported |
|---|---|
| `v0.x` (current) | yes |
| `< v0.x` | no |

Once a `v1.0` line ships, the matrix here will document the
supported `vN.x` lines explicitly.

## Reporting a vulnerability

**Do not open a public GitHub issue for a security-relevant finding.**

Use one of:

1. **GitHub Private Vulnerability Reporting** (preferred):
   <https://github.com/RandomCodeSpace/ctm/security/advisories/new>
   — opens a private channel between you and the maintainer with
   built-in CVE reservation and disclosure workflow.
2. **Email**: open an issue at <https://github.com/RandomCodeSpace/ctm/issues>
   marked "request: private security contact" — the maintainer will
   reply with a private channel within 14 days. Use this path only
   if GitHub's private advisory mechanism is unavailable to you.

## What to include

The faster the maintainer can reproduce the issue, the faster a fix
ships. Please include:

- **Affected versions** (output of `ctm version`).
- **Environment**: OS, tmux version, shell, whether ctm is reachable
  from outside `127.0.0.1` (e.g. via reverse proxy).
- **Impact**: what an attacker can do — RCE, info disclosure,
  privilege escalation, DoS, etc.
- **Reproducer**: smallest sequence of commands or HTTP requests
  that triggers the issue.
- **Suggested fix** if you have one — appreciated but not required.
- **Whether you intend to seek a CVE** — the maintainer can help
  reserve one through GitHub's advisory flow.

## Response commitment

| Stage | Target |
|---|---|
| Acknowledge receipt | within **14 days** |
| Initial assessment + severity rating | within **30 days** |
| Fix or mitigation merged to `main` | within **60 days** for High/Critical; longer for Low/Medium with explicit agreement from the reporter |
| Public disclosure (advisory + release notes) | by mutual agreement, default **90 days** from receipt or on first patched release, whichever is sooner |

These are best-effort targets for a single-maintainer project. If
you have not heard back within the acknowledgement window, please
ping the same channel — mail can get lost.

## What is in scope

In scope:

- The `ctm` CLI and all subcommands (`yolo`, `safe`, `attach`,
  `serve`, `kill`, etc.).
- The `ctm serve` HTTP daemon and its API + UI surface.
- Any session-state, log, or auth file ctm writes under
  `~/.config/ctm/` or `~/.claude/`.
- Build-time supply-chain integrity (vendored deps, release
  artifacts).

Out of scope:

- Bugs in tmux, claude, git, or any other binary ctm shells out to.
- Configuration mistakes by the operator (e.g. binding `ctm serve`
  to `0.0.0.0` without an authenticating reverse proxy).
- Findings that require a pre-compromised local user account on the
  same machine where ctm runs (ctm trusts the user it runs as).

## Security architecture quick reference

For background while reviewing a report:

- ctm binds **`127.0.0.1` only** by default. External exposure is
  the operator's reverse-proxy responsibility.
- Single-user authentication uses **argon2id** (RFC 9106) with
  parameters meeting OWASP recommendations. See V27 spec.
- Session tokens are **256-bit** values from `crypto/rand`, stored
  hashed.
- Mutation endpoints require both a bearer token **and** an Origin
  header allow-list (see `internal/serve/api/`).
- All git, tmux, and claude invocations resolve binaries through
  `$PATH` — see `sonar-project.properties` for the documented
  threat model behind that.

## Public disclosures to date

None. This file will list past advisories under their CVE IDs as
they are issued.

## Credit

We credit reporters in the release notes of the patched release
unless you ask to remain anonymous. Please tell us how you'd like to
be credited (name + handle / org / preferred link).
