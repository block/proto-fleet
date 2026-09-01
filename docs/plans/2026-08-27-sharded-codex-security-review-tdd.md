---
title: "Sharded Codex security review"
date: 2026-08-27
status: accepted
type: tdd
tracker: https://github.com/block/proto-fleet/pull/980
---

# Sharded Codex security review

## Context

The [bounded-review benchmark](../codex-security-review-benchmark-report.md)
showed that diff context, reasoning effort, and prompt guidance do not make the
current single-agent review complete reliably. Only 22 of 72 adjudicated matrix
cases completed; the other 50 exhausted the 12-minute outer budget. Compact
context downgraded a known `HIGH` in two of three trials, `medium` effort
recalled only one of four expected `MEDIUM` findings in completed reviews and
produced an invalid new `HIGH`, and the bounded prompt also downgraded the known
`HIGH`. Production therefore remains on `unified=40`, `xhigh`, and the baseline
prompt.

The existing reviewer has one model process inspect the full exact diff and any
unchanged files it chooses. Large cross-component changes can therefore consume
the entire budget before producing output. The next experiment should bound the
amount of changed code assigned to each model invocation without hiding shared
contracts or relying on a second unbounded model pass.

## Goals

- Complete normal and large semantic reviews within the existing end-to-end
  production bound.
- Across completed reviews, preserve every adjudicated `HIGH` and at least 90%
  of adjudicated `MEDIUM` findings without severity downgrade.
- Preserve the exact three-dot diff, pinned SHAs, prompt-injection boundary,
  read-only sandbox, and fail-closed artifact contract.
- Keep cross-component contract changes visible to every affected shard.
- Aggregate shard results deterministically without another model invocation.
- Bound model concurrency and total API cost.

## Non-goals

- Arbitrary file-count or byte-count sharding that separates related code.
- Lowering reasoning effort, reducing diff context, or adopting the bounded
  prompt in the first sharding experiment.
- Allowing partial shard success to authorize approval-free merge.
- Semantic deduplication by a second model.
- Replacing human adjudication of benchmark findings.

## Design

### Trusted shard planner

A trusted workflow step reads the exact changed-file list and assigns files to
stable architecture domains. The table is evaluated top to bottom and the first
match wins, so ownership is deterministic and mutually exclusive:

| Precedence | Domain | Primary paths |
| ---: | --- | --- |
| 1 | Delivery and repository infrastructure | `.github/`, `deployment-files/`, `docs/`, `scripts/`, `server/monitoring/`, every `Dockerfile*` and Compose file, and root-level tooling/configuration |
| 2 | Contracts and persistence | `proto/`, `server/migrations/`, `server/sqlc/`, `server/generated/sqlc/`, `server/sdk/v1/pb/`, generated protobuf files, and generated API boundaries |
| 3 | ProtoFleet | `client/src/protoFleet/` |
| 4 | ProtoOS | `client/src/protoOS/` |
| 5 | Shared client contracts | Remaining `client/`, including `client/src/shared/`; replicated as context to every affected client-app shard |
| 6 | Rust plugin SDK | `sdk/rust/`; replicated to affected ASIC-RS shards |
| 7 | ASIC-RS | `plugin/asicrs/` |
| 8 | Go/Python plugins | Remaining `plugin/` and plugin-generator packages |
| 9 | Server | Remaining `server/` |
| 10 | Cross-cutting | Every remaining path |

The planner is path-based, versioned on the trusted default branch, and emits a
machine-readable manifest containing the exact base/head SHAs, computed
three-dot merge-base SHA, every changed file, its owning shard, and
shared-contract membership. The final cross-cutting
rule ensures unknown paths are reviewed rather than dropped.

Within each domain, the planner forms indivisible semantic units: Go packages,
client feature directories, protobuf packages, a migration with its sqlc
contract, plugin modules, or one deployment surface. It never splits a file or
hunk. The planner then collects units from every domain into one review-wide
pool and assigns that pool to no more than two shard packets total, including
replicated context. Domains do not receive separate packet allowances. Each of
the two review-wide packets has both of these hard limits:

- 500,000 bytes per complete packet.
- 12,500 lines per complete packet.

These initial limits are benchmark hypotheses. A planner dry run showed that PR
#956 contains one indivisible 12,173-line lockfile unit. The line limit admits
that unit, while the 500,000-byte limit still splits the known 812,319-byte,
13,926-line control into two packets and keeps the entire review in one parallel
wave. If one semantic unit exceeds either limit, shared
context makes a packet exceed a limit, or the review-wide pool requires more
than two bounded packets, the planner does not launch an unbounded model job. It
also fails closed above 750 semantic units or 2,000 explored partition states,
so pathological packing cannot exhaust the trusted planner job. These cases emit
a validated `oversized-review` incomplete result that requires human review and
records the limiting units, safety bound, and packet measurements.

### Shared review context

Each shard receives:

1. The complete changed-file manifest, per-file diff statistics, actual added-line
   ranges, and merge-base-side deleted-line ranges for whole-file deletions.
2. The `unified=40` diff for files owned by that shard.
3. Changed shared contracts relevant to that shard, even when another shard
   owns them. Shared inputs include protobuf definitions, migrations and sqlc
   interfaces, API types, deployment schemas, root build/runtime contracts, and
   the canonical plugin proto plus Rust SDK required by ASIC-RS.
4. The production model, security boundary, output schema, read-only sandbox,
   and common review guidance.
5. A trusted shard-scope prompt stanza stating that the packet is the complete
   authorized scope for this shard, not the complete PR diff; primary files are
   its review ownership and shared files are supporting cross-boundary context.
   It instructs the model not to regenerate the full PR diff and to report only
   findings grounded in a primary change or its interaction with shared context.

The packet records primary and shared files separately so duplicated contract
context cannot be mistaken for duplicate review ownership. Every changed file
must appear as primary in exactly one shard. The finalizer rejects manifests
with missing, multiply owned, stale-SHA, or out-of-range files and remeasures
each complete packet against both hard size limits. Finding locations must cite
a primary-owned changed file, never replicated shared context directly, and an
actual added line in the head revision; hunkless metadata, binary, and empty-file
changes expose no valid finding location. Deletion-only hunks in surviving files
use a documented nearest-surviving-line anchor, while whole-file deletions and
truncations to empty cite their actual removed lines in the exact three-dot
merge-base revision.

### Bounded shard execution

Run shard reviewers as one matrix wave with `max-parallel: 2`. Because the
planner emits no more than two packets for the whole review, no second model
wave can extend the deadline. A trusted preparation job with no model secret
builds each packet and a compressed exact-head worktree. It excludes all Git
metadata, extra refs, and the benchmark corpus before handing the archive,
manifest, packet, and rendered prompt to the model job, so the model cannot
recover omitted hunks or adjudication labels.

Each isolated model job has an enforceable six-minute outer timeout; full
repository history needed for the exact three-dot merge base exists only in the
secretless preparation job. The pinned
composite action remains byte-for-byte aligned with the baseline action contract,
but cannot consume model API time beyond the model-only job boundary even though
it ignores caller-step timeout. A separate trusted finalizer runs after GitHub
cancellation cleanup. It accepts timeout evidence only when every model-job
prerequisite succeeded and the Codex step itself was still cancelled or in
progress; cancellation after successful model completion is an automation
failure. Production timing is chosen only after benchmark evidence; completed
runs must keep the single parallel wave, trusted aggregation, and artifact upload
inside the current 15-minute end-to-end target.

Each shard emits the existing structured risk and Markdown contract plus trusted
metadata identifying its shard, exact range, primary files, shared files, run
ID, completion status, and elapsed time. Early, manual, superseded, setup, and
ambiguous cancellation remain hard failures. A verified shard budget timeout or
planner-produced `oversized-review` result is validated incomplete evidence,
not a successful empty result.

### Deterministic aggregation

A trusted aggregation job runs after every shard finalizer and does not execute
code from the reviewed checkout. It:

- Verifies the manifest, exact SHAs, run identity, and one result per shard.
- Adds a synthetic `HIGH` fallback finding only for validated incomplete
  evidence, such as a verified shard budget timeout, finalizer-normalized
  unusable model output, or planner-produced `oversized-review`; completed shard
  findings retain their reported severity, and the fallback states which
  domains require human review.
- Hard-fails the workflow when a trusted artifact is missing, corrupt,
  malformed, stale, or bound to another run. Artifact upload and finalizer
  failures never become normal aggregate findings.
- Computes overall risk as the maximum shard severity.
- Concatenates findings in severity, shard, path, and line order.
- Preserves potentially duplicate findings rather than using a model or fuzzy
  matching to remove them. Exact duplicate structured entries may be collapsed
  only by a documented deterministic fingerprint.
- Emits the existing production artifact and comment contract so review policy
  continues to consume one trusted result.
- Uploads incomplete evidence before failing the case, ensuring any timeout,
  invalid output, or `oversized-review` makes the reusable case and parent corpus
  roll-up fail rather than appearing as a green 6/6 run.

No shard may post directly to a pull request. Only the aggregator posts after
revalidating that the PR head is current.

### Benchmark sequence

1. Add a manual `repository_dispatch` benchmark mode that uses only fixed corpus
   SHAs and trusted default-branch code.
2. Replay the adjudicated corpus with `unified=40`, `xhigh`, and the baseline
   prompt. Compare against the unsharded control in the benchmark report.
3. Require a completed aggregate for all six adjudicated corpus cases in the
   initial candidate run. Any timeout or `oversized-review` rejects the
   candidate before recall evaluation; hard finding-bearing cases cannot
   disappear from the denominator.
4. Human-adjudicate the union of shard findings. Across completed reviews,
   require every known `HIGH` and at least 90% of known `MEDIUM` findings.
   Require every newly reported `MEDIUM` or `HIGH` to be valid across the full
   corpus, not only the clean controls.
5. Repeat only disagreements, misses, or cases within 10% of a shard budget to
   measure model variance. Repeats cannot erase a failure of the initial 6/6
   completion gate.
6. If the adjudicated gates pass, replay PRs #957 and #964. Both must produce a
   credible completed aggregate within 10 minutes and the final artifact within
   15 minutes. A validated timeout or `oversized-review` artifact demonstrates
   safe fallback behavior but blocks rollout because it does not improve the
   large-PR completion failure.
7. Only after both large-PR cases pass, roll out behind a workflow-level switch
   that can return production to the single-agent path without weakening its
   current timeout or fail-closed behavior.

## Alternatives considered

- **More single-agent prompt tuning:** Rejected because the bounded prompt had
  the same 2/6 completion profile and downgraded an adjudicated `HIGH`.
- **Lower reasoning effort:** Rejected because `high` completed 2/6 cases;
  `medium` completed 5/6 but recalled only one of four expected `MEDIUM`
  findings in completed reviews, while also producing an invalid new `HIGH`.
- **Smaller global diff context:** Rejected after three trials per variant. Every
  variant missed all 15 expected `MEDIUM` observations, and compact context
  downgraded a known `HIGH` in two trials.
- **Arbitrary equal-size shards:** Rejected because they can hide transaction,
  API, and lifecycle relationships that cross files.
- **Model-based aggregation:** Rejected because it adds another unbounded pass
  and another opportunity to omit or downgrade findings.
- **Longer timeout:** Rejected because it restores the operational failure this
  work is intended to prevent.

## Risks

| Risk | Mitigation |
| --- | --- |
| A cross-domain bug is hidden | Replicate changed shared contracts and include the complete changed-file manifest in every shard |
| Replicated context increases cost | Create only affected shards, cap the whole review at two packets, include shared bytes in the hard packet limits, and measure duplication |
| A shard regenerates the full PR diff | State trusted primary/shared scope explicitly in the prompt and test that the full-diff instruction is absent |
| Findings are duplicated | Preserve them by default; allow only exact deterministic fingerprint deduplication |
| One shard timeout is masked | Any validated incomplete shard forces an aggregate `HIGH` human-review result |
| Trusted handoff failure looks like model incompletion | Hard-fail missing, corrupt, stale, cross-run, or malformed artifacts; accept only validated incomplete evidence |
| One semantic unit or planner search exceeds a safety bound | Skip the model and emit a measured `oversized-review` human-review result instead of running unbounded |
| Path rules overlap or silently omit files | Apply first-match precedence, require exact one-to-one primary ownership, and route unmatched paths to a cross-cutting shard |
| Matrix output is mixed across runs | Bind every shard artifact to run ID, shard ID, exact base/head SHAs, and manifest digest |
| Aggregation executes untrusted code | Use inline trusted default-branch logic without a PR checkout |
| Parallelism increases API spend | Start at `max-parallel: 2`, record per-shard tokens, and cap the number of shards |
| Rollout regresses review quality | Keep the single-agent configuration as rollback and require adjudicated recall before rollout |

## Test plan

- Unit-test first-match path ownership, unknown-path fallback, semantic-unit
  grouping, deterministic packing, shared-contract replication, and exact
  one-to-one changed-file coverage.
- Test both packet limits at, below, and above the boundary, including a single
  oversized unit, replicated context overflow, a bounded partition-state search,
  and a change requiring more than two packets. Validate added-line-only ranges,
  deletion anchors, and merge-base-revision links for whole-file deletions.
- Assert byte-identical model, security boundary, schema, common review guidance,
  sandbox, and shard-scope prompt stanza between production and benchmark shard
  jobs. Test that the stanza identifies primary/shared scope and prohibits
  regenerating the full PR diff.
- Test aggregate severity, closed finding categories, stable ordering, exact
  deduplication, and validated timeout/oversized `HIGH` behavior with measured
  elapsed time. Separately prove missing, corrupt,
  malformed, stale, and cross-run artifacts hard-fail the workflow.
- Mock Actions APIs to distinguish verified budget timeout from setup, manual,
  infrastructure, and supersession cancellation.
- Run actionlint, Ruff, workflow-policy tests, and executable mocked posting
  tests.
- Replay the fixed adjudicated corpus and require 6/6 initial completion before
  evaluating packet size, duplicated bytes, wall time, tools, tokens,
  compactions, human finding recall, and the validity of every new `MEDIUM` or
  `HIGH`.
- Run the large-PR corpus only after the adjudicated recall and finding-validity
  gates pass; require both cases to complete credibly before rollout.
- Observe the first 30 production runs before removing the single-agent rollback
  path.
