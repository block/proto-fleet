# Dependencies and tool setup

## Go workspace changes

After changing dependencies in a workspace member (`server/`, `plugin/`, or
`tests/`), or editing `go.work` / `go.work.sum`, run `go work sync` from the
repo root. Include resulting workspace lock changes with the module changes.
The member list is authoritative in `go.work`.

## Tool setup

[CONTRIBUTING.md](../../CONTRIBUTING.md#development-setup) supports Hermit
and non-Hermit setups. Preserve the user's chosen setup.

For Hermit, source `bin/activate-hermit` from the repo root. Tool proxies live
in `bin/`; activation and proxies resolve installed packages through Hermit.
Do not assume every tool's executable lives under `.hermit/`. For missing
tools or activation errors, use the
[Hermit troubleshooting skill](../../.claude/skills/hermit-tooling/SKILL.md).
Do not mask a broken Hermit setup by installing global replacements.

For non-Hermit setups, follow CONTRIBUTING.md's prerequisites and
area-specific setup recipes. Adding a new tool still requires approval.
