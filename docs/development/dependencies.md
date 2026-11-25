# Dependencies and tool setup

## Go workspace changes

After changing dependencies in a workspace member (`server/`, `plugin/`,
`sdk/mdk/`, or `tests/`), or editing `go.work` / `go.work.sum`, run `go work sync` from the
repo root. Include resulting workspace lock changes with the module changes.
The member list is authoritative in `go.work`.

## Dependency updates

Dependabot opens one monthly pull request per area: `server` (Go modules,
Cargo lockfiles, Dockerfiles, and Compose images), `client` (npm and the
client nginx image), and GitHub Actions. HA Patroni requirements update
quarterly because they feed the HA release compatibility fingerprint.
Security updates are grouped per ecosystem. Major versions and `golang`
images are excluded; see `.github/dependabot.yml`.

Docker and Compose images receive patch releases and digest refreshes only.
A newer minor release of an image, such as a later Alpine branch, a
non-LTS Ubuntu release, or a new etcd or OpenTelemetry collector minor
version, can change runtime behavior, so move to it in a dedicated pull
request after checking the release notes. The OpenTelemetry collector image
is updated by hand entirely, because Dependabot mistakes its
single-architecture tags (such as `0.150.1-386`) for patch releases.

Dependabot cannot move the Go toolchain. When a module requires a newer Go
release, update the Hermit `go` package, the `go` line in `go.work`, and every
`golang:` base image together in the dependency pull request.

## Vulnerability scanning

`just vuln-scan` runs `govulncheck` in every Go module and `osv-scanner` on
the npm, Cargo, and pip lockfiles. The PR Gate runs it when dependency files
change, and it also runs weekly. `govulncheck` reports only vulnerabilities
the code can reach, including Go standard library issues that Dependabot does
not track. Accepted lockfile findings live in `osv-scanner.toml` with a reason
and an `ignoreUntil` date; when an entry expires, the scan fails until it is
re-reviewed.

## Tool setup

[CONTRIBUTING.md](../../CONTRIBUTING.md#development-setup) supports Hermit
and non-Hermit setups. Preserve the user's chosen setup.

For Hermit, source `bin/activate-hermit` from the repo root. Tool proxies live
in `bin/`; activation and proxies resolve installed packages through Hermit.
Do not assume every tool's executable lives under `.hermit/`. For missing
tools or activation errors, use the
[Hermit troubleshooting skill](../../.agents/skills/hermit-tooling/SKILL.md).
Do not mask a broken Hermit setup by installing global replacements.

For non-Hermit setups, follow CONTRIBUTING.md's prerequisites and
area-specific setup recipes. Adding a new tool still requires approval.
