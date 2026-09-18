# Agent guidance validation — 2026-09-18

Compared the revised guidance with repository baseline
`fd35e6d07f025a4e1f6db221daa39ecccba42457`. This is a documentation review and
instruction-level back-test, not a measured agent benchmark or application
test run. Baseline files can be recovered with `git show <baseline>:<path>`.

## Scope and size

| Measure | Baseline | Revised |
| --- | ---: | ---: |
| Root AGENTS.md lines | 215 | 86 |
| Repository skills | 15 | 10 |
| Skill description words | 619 | 111 |
| E2E skill entry-point lines | 248 | 53 |

Line and whitespace-delimited word counts measure the discovery/entry-point
surface, not total prompt tokens. Some guidance moved to scoped files and
references; these figures do not imply all of it was deleted.

## Review and verification

- An independent read-only review checked all 12 original root invariants
  against the new root, scoped guidance, and linked skills. No lost invariant
  was identified after corrections.
- All 10 skills passed the skill-creator `quick_validate.py` validator.
  Command metadata and rendered template frontmatter parsed as YAML.
- Local Markdown file/directory links and heading anchors in changed guidance
  resolved. Removed skill names were checked for dangling references; seven
  existing plan references now point by name to `code-generation`.
- `git diff --check` passed. The existing client Prettier check passed for
  `client/AGENTS.md` when run from `client/` so its configured plugin resolved.
- Build/setup claims were checked against the root/server/package `justfile`s,
  Hermit activation/proxies, Compose definitions, E2E target configuration,
  and the contract harness. No new tooling was installed.
- A disposable two-edit patch fixture exercised `git apply --numstat`:
  aggregate diff returned `+1/-1` for one file; per-commit patches returned
  two `+1/-1` rows for that same file. This verifies the counting pitfall
  behind the revised aggregate-diff instruction. GitHub CLI's
  [diff implementation](https://github.com/cli/cli/blob/trunk/pkg/cmd/pr/diff/diff.go)
  selects different diff and patch media types; no live PR was edited.

Corrections found during review: a relative link depth error, an unquoted YAML
colon, incomplete PR traversal metadata (`title` and child `headRefName`),
stale host-networking guidance, and confusion between native ASIC-rs builds
and container-based contract execution. Draft/audit-only PR requests now
explicitly preserve their read-only scope.

## Before/after scenario back-test

An independent agent traced the following requests through baseline and
revised instructions and checked relevant source files. These are simulated
decisions, not executed feature implementations. Old conflicting directions
could already be overridden by user or higher-priority instructions; the
comparison does not assume agents necessarily followed the problematic text.

| Request / fixture | Baseline behavior or ambiguity | Revised decision / completion boundary |
| --- | --- | --- |
| Fix a README typo | Ordinary edit is small; invoking readiness adds unconditional application lint. | Relevant text/link/format checks; no application suite for documentation-only readiness. |
| Add a mode-specific optional proto request knob | Root contract rules plus separate generation skill. | Reject the knob outside its consuming mode, update consumers, regenerate and validate; structural-only response rules preserved. |
| Correct a merged migration | Immutable, but skill says to propose the corrective migration. | Implement a new up/down pair, regenerate and verify; leave the merged file intact. |
| Revise an undeployed, unmerged migration | In-place edits allowed; existing-migration generation trigger is unclear. | Revise original pair and regenerate, including schema-only changes. |
| Change Python generator and bundled pip-config | Packaging required; readiness separately repeats package tests. | Preserve tarball/version requirements; recognize that `just package` already includes tests. |
| Add E2E coverage with a real target; alternate fake target has stale auth | Try current environment first; mandatory setup and publishing questions. | Resolve target before mutation; repair authorized local fake setup; real/shared effects need matching authorization. |
| Failed assertion suggests refreshing screenshots | Root snapshot consent required. | Same explicit replacement consent and image-review reminder; test output cannot authorize refresh. |
| Finish a plan and mark completed | Root says archive; skill says only remind the user. | Complete acceptance criteria, update status and archive in the same authorized operation; preserve links. |
| Implement, check, commit/push/open PR; own unused import fails lint | Readiness tells agent to stop for user repair. | Fix own failure, rerun affected checks, continue authorized publication after passing checks. |
| Audit readiness only, no edits | Starts with main-based diff; resolves stack scope later. | Resolve actual base first, inspect/report without edits or publication. |
| Describe a cross-repo stacked PR while local HEAD is unrelated | Target isolation exists; recursive metadata incomplete and per-commit counting can inflate size. | Preserve target isolation/full stack context, collect needed metadata, use aggregate counts; draft-only remains read-only. |
| Verify ASIC-rs after macOS branch switch | Forces Docker rebuild then contract dependency switches back to native. | Distinguish native/Docker artifacts and report the current macOS contract recipe blocker. |

No hard-invariant regression was found in these twelve scenarios. This does
not establish model pass rates, latency savings, or performance across every
agent that may consume the repository instructions.

## Remaining limitation

The current `test-contract` recipe depends on `_asicrs-build`, which produces
a macOS-native binary on Darwin, then executes the plugin inside Linux
containers. The revised skill documents this source-confirmed platform
mismatch and directs contract verification to a compatible authorized Linux
runner. The recipe itself was not changed or executed in this documentation
task. Application suites, real-miner operations, screenshot refreshes, and
publishing were outside this verification run.
