---
name: pr-ready
description: Verify a branch, resolve in-scope failures, and prepare its PR description; publish only when requested.
argument-hint: "(optional: audit only, or explicit request to open the PR)"
---

# PR readiness

Establish the actual target and immediate base using
[/pr-describe](../pr-describe/SKILL.md)'s target-resolution guidance before assessing
the diff. Include working-tree changes in local readiness checks; do not use
`main...HEAD` for a stacked PR. A remote PR whose branch is not checked out
can be reviewed, but local test results are not evidence for that remote head.

Complete applicable checks and produce a description following the
[PR standard](../../../docs/development/pr-descriptions.md). For implementation
work, fix lint/test/generation failures caused by the requested change and
rerun affected checks. For an explicit audit-only request, inspect and report
without editing. Report unrelated failures or unavailable prerequisites with
their impact; do not silently call blocked validation a pass.

## Check selection

Use the `justfile`s and affected behavior to choose targeted tests while
editing. For an authorized push, let the enabled pre-push hook run
`just check-changed` as the final broad changed-path validation; do not run
the same command manually immediately before pushing. For local-only
completion, run `just check-changed` manually once the edits are complete.
Pre-push does not replace relevant targeted tests. Fix hook failures caused
by the change and retry the push with hooks enabled; report unrelated failures
or unavailable prerequisites as validation gaps.
Do not run `just lint` unless a full-repository check is specifically needed.
Documentation-only changes need link, instruction, and formatting checks
rather than application suites.

| Changed area | Verification |
| --- | --- |
| Server behavior | `cd server && just test`, or affected Go packages |
| Client behavior | Affected Vitest tests via `cd client && npm test -- --run`; typecheck when types/imports change |
| Miner protocol or shared fake rigs | `just test-contract`; relevant E2E when changed behavior is consumed there |
| ASIC-rs, Rust SDK, or `server/sdk/v1/pb/` | [ASIC-rs build guidance](../asicrs-build/SKILL.md), then affected contract coverage |
| Proto/schema/query or generator config | [Generation skill](../code-generation/SKILL.md), affected consumer tests |
| Python generator | [Packaging skill](../python-gen-tarball/SKILL.md); `just package` includes its tests |
| Python SDK | `cd server/sdk/v1/python && just test` |
| Playwright coverage | [E2E skill](../proto-fleet-playwright-e2e/SKILL.md) |

Check applicable scoped guidance for client boundaries, migration
immutability, Go workspace sync, and generated artifacts. Do not load every
skill. Repeat passing checks only after relevant inputs change or a new
concern appears; required hooks remain enabled on every push.

## Finish

Report remaining risks and validation gaps, and prepare the description.
Commit, push, or open the PR only when requested by the user. Existing
authorization carries forward; do not ask again after successful checks.
Do not publish while required checks remain failed or blocked; report the
blocker and the decision needed. Verify the feature branch before committing,
preserve hooks, and pass PR descriptions through `--body-file`.
