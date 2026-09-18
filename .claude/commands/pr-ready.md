---
description: Verify a branch, resolve in-scope failures, and prepare its PR description; publish only when requested.
argument-hint: "(optional: audit only, or explicit request to open the PR)"
---

# PR readiness

Establish the actual target and immediate base using
[/pr-describe](pr-describe.md)'s target-resolution guidance before assessing
the diff. Include working-tree changes in local readiness checks; do not use
`main...HEAD` for a stacked PR. A remote PR whose branch is not checked out
can be reviewed, but local test results are not evidence for that remote head.

Complete applicable checks and produce a description following the
[PR standard](../../docs/development/pr-descriptions.md). For implementation
work, fix lint/test/generation failures caused by the requested change and
rerun affected checks. For an explicit audit-only request, inspect and report
without editing. Report unrelated failures or unavailable prerequisites with
their impact; do not silently call blocked validation a pass.

## Check selection

Use the `justfile`s and affected behavior to choose checks. For code changes,
run `just lint` and relevant tests; documentation-only changes need link,
instruction, and formatting checks rather than application suites.

| Changed area | Verification |
| --- | --- |
| Server behavior | `cd server && just test`, or affected Go packages |
| Client behavior | Affected Vitest tests via `cd client && npm test -- --run`; typecheck when types/imports change |
| Miner protocol or shared fake rigs | `just test-contract`; relevant E2E when changed behavior is consumed there |
| ASIC-rs, Rust SDK, or `server/sdk/v1/pb/` | [ASIC-rs build guidance](../skills/asicrs-build/SKILL.md), then affected contract coverage |
| Proto/schema/query or generator config | [Generation skill](../skills/code-generation/SKILL.md), affected consumer tests |
| Python generator | [Packaging skill](../skills/python-gen-tarball/SKILL.md); `just package` includes its tests |
| Python SDK | `cd server/sdk/v1/python && just test` |
| Playwright coverage | [E2E skill](../skills/proto-fleet-playwright-e2e/SKILL.md) |

Check applicable scoped guidance for client boundaries, migration
immutability, Go workspace sync, and generated artifacts. Do not load every
skill or rerun already-passing checks without a new change or unresolved risk.

## Finish

Report remaining risks and validation gaps, and prepare the description.
Commit, push, or open the PR only when requested by the user. Existing
authorization carries forward; do not ask again after successful checks.
Do not publish while required checks remain failed or blocked; report the
blocker and the decision needed. Verify the feature branch before committing,
preserve hooks, and pass PR descriptions through `--body-file`.
