---
title: "Sharded Codex security review"
date: 2026-08-27
status: draft
type: tdd
tracker:
---

# Sharded Codex security review

## Context

The [bounded-review benchmark](../codex-security-review-benchmark-report.md)
showed that diff context, reasoning effort, and prompt guidance do not make the
current single-agent review complete reliably. Only 22 of 72 adjudicated matrix
cases completed; the other 50 exhausted the 12-minute outer budget. Compact
context downgraded a known `HIGH` in two of three trials, `medium` effort
recalled only one of five known `MEDIUM` findings and produced an invalid new
`HIGH`, and the bounded prompt also downgraded the known `HIGH`. Production
therefore remains on `unified=40`,
`xhigh`, and the baseline prompt.

The existing reviewer has one model process inspect the full exact diff and any
unchanged files it chooses. Large cross-component changes can therefore consume
the entire budget before producing output. The next experiment should bound the
amount of changed code assigned to each model invocation without hiding shared
contracts or relying on a second unbounded model pass.

## Goals

- Complete normal and large semantic reviews within the existing end-to-end
  production bound.
- Preserve every adjudicated `HIGH` and at least 90% of adjudicated `MEDIUM`
  findings without severity downgrade.
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
stable architecture domains:

| Domain | Primary paths |
| --- | --- |
| Contracts and persistence | `proto/`, migrations, sqlc queries, generated API boundaries |
| Server | `server/` excluding shared contract inputs |
| Shared client contracts | `client/src/shared/`; replicated as context to every affected client-app shard |
| ProtoFleet | `client/src/protoFleet/` |
| ProtoOS | `client/src/protoOS/` |
| Go/Python plugins | `plugin/` except `plugin/asicrs/`, generator packages |
| ASIC-RS | `plugin/asicrs/` |
| Delivery and infrastructure | `.github/`, Docker, deployment, monitoring, scripts, root tooling |

The planner is path-based, versioned on the trusted default branch, and emits a
machine-readable manifest containing the exact base/head SHAs, every changed
file, its owning shard, and shared-contract membership. Unknown paths go to a
conservative cross-cutting shard rather than being dropped.

Create a shard only when its primary domain has changed. Cap the first
experiment at two shards so both model jobs can run in one bounded parallel
wave. If more than two domains change, combine adjacent domains according to a
checked-in deterministic merge order; never split a domain merely to equalize
bytes.

### Shared review context

Each shard receives:

1. The complete changed-file manifest and per-file diff statistics.
2. The `unified=40` diff for files owned by that shard.
3. Changed shared contracts relevant to that shard, even when another shard
   owns them. Shared inputs include protobuf definitions, migrations and sqlc
   interfaces, API types, deployment schemas, and root build/runtime contracts.
4. The same trusted prompt, model, security boundary, output schema, and
   read-only sandbox as production.

The packet records primary and shared files separately so duplicated contract
context cannot be mistaken for duplicate review ownership. Every changed file
must appear as primary in exactly one shard. The finalizer rejects manifests
with missing, multiply owned, stale-SHA, or out-of-range files.

### Bounded shard execution

Run shard reviewers as a matrix with bounded parallelism. The benchmark should
start with a six-minute model budget per shard and reserve the existing
five-minute Actions cancellation-cleanup allowance. Production timing is chosen
only after benchmark evidence; it must keep trusted aggregation and artifact
upload inside the current 15-minute end-to-end target.

Each shard emits the existing structured risk and Markdown contract plus trusted
metadata identifying its shard, exact range, primary files, shared files, run
ID, completion status, and elapsed time. Early, manual, superseded, setup, and
ambiguous cancellation remain hard failures. A verified shard budget timeout is
an incomplete review, not a successful empty result.

### Deterministic aggregation

A trusted aggregation job runs after every shard finalizer and does not execute
code from the reviewed checkout. It:

- Verifies the manifest, exact SHAs, run identity, and one result per shard.
- Fails closed to overall `HIGH` when any shard is incomplete, malformed, or
  missing; the review states which domains require human review.
- Computes overall risk as the maximum shard severity.
- Concatenates findings in severity, shard, path, and line order.
- Preserves potentially duplicate findings rather than using a model or fuzzy
  matching to remove them. Exact duplicate structured entries may be collapsed
  only by a documented deterministic fingerprint.
- Emits the existing production artifact and comment contract so review policy
  continues to consume one trusted result.

No shard may post directly to a pull request. Only the aggregator posts after
revalidating that the PR head is current.

### Benchmark sequence

1. Add a manual `repository_dispatch` benchmark mode that uses only fixed corpus
   SHAs and trusted default-branch code.
2. Replay the adjudicated corpus with `unified=40`, `xhigh`, and the baseline
   prompt. Compare against the unsharded control in the benchmark report.
3. Human-adjudicate the union of shard findings. Require every known `HIGH`, at
   least 90% of known `MEDIUM` findings, and no invalid `HIGH` on either clean
   control.
4. Repeat only disagreements, misses, or cases within 10% of a shard budget.
5. If the adjudicated gate passes, replay PRs #957 and #964. Require a credible
   aggregate review or an explicit incomplete `HIGH` artifact within 15 minutes.
6. Roll out behind a workflow-level switch that can return production to the
   single-agent path without weakening its current timeout or fail-closed
   behavior.

## Alternatives considered

- **More single-agent prompt tuning:** Rejected because the bounded prompt had
  the same 2/6 completion profile and downgraded an adjudicated `HIGH`.
- **Lower reasoning effort:** Rejected because `high` completed 2/6 cases;
  `medium` completed 5/6 but recalled only one of five known `MEDIUM` findings
  and one of two known `HIGH` findings, while also producing an invalid new
  `HIGH`.
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
| Replicated context increases cost | Create only affected shards, cap concurrency, and measure duplicated packet bytes |
| Findings are duplicated | Preserve them by default; allow only exact deterministic fingerprint deduplication |
| One shard timeout is masked | Any incomplete shard forces an aggregate `HIGH` human-review result |
| Path rules silently omit files | Require exact one-to-one primary ownership and route unknown paths to a cross-cutting shard |
| Matrix output is mixed across runs | Bind every shard artifact to run ID, shard ID, exact base/head SHAs, and manifest digest |
| Aggregation executes untrusted code | Use inline trusted default-branch logic without a PR checkout |
| Parallelism increases API spend | Start at `max-parallel: 2`, record per-shard tokens, and cap the number of shards |
| Rollout regresses review quality | Keep the single-agent configuration as rollback and require adjudicated recall before rollout |

## Test plan

- Unit-test path ownership, unknown-path fallback, deterministic domain merging,
  shared-contract replication, and exact one-to-one changed-file coverage.
- Assert byte-identical security boundary, schema, sandbox, model, and prompt
  between production and benchmark shard jobs.
- Test aggregate severity, stable ordering, exact deduplication, and fail-closed
  behavior for missing, malformed, stale, timed-out, and cross-run artifacts.
- Mock Actions APIs to distinguish verified budget timeout from setup, manual,
  infrastructure, and supersession cancellation.
- Run actionlint, Ruff, workflow-policy tests, and executable mocked posting
  tests.
- Replay the fixed adjudicated corpus and record packet size, duplicated bytes,
  completion, wall time, tools, tokens, compactions, and human finding recall.
- Run the large-PR corpus only after the adjudicated recall gate passes.
- Observe the first 30 production runs before removing the single-agent rollback
  path.
