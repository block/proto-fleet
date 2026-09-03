---
title: "Extended Codex security review timeout benchmark"
date: 2026-09-03
status: completed
type: plan
tracker: https://github.com/block/proto-fleet/pull/1007
---

# Extended Codex security review timeout benchmark

## Context

The first 30 production runs of the bounded Codex reviewer produced three
completed reviews and 13 verified model-timeout fallbacks. Eight other runs were
verified superseded and six ambiguous cancellations failed hard. Completed
reviews finished quickly: the production maximum was 140 seconds, and all 22
completed Sol tuning cases finished within 172 seconds. By contrast, the
12-minute Sol control completed only four of 18 benchmark cases.

That distribution is bimodal rather than a demonstrated long tail: observed
reviews either completed within roughly three minutes or exhausted their model
budget. A longer timeout may still be worthwhile because an automated review is
valuable, but production should not absorb more latency until a fixed,
adjudicated experiment shows that extra model time converts timeouts into valid
reviews.

The [bounded-review benchmark report](../../codex-security-review-benchmark-report.md)
records the retained nine-minute production outer reviewer budget, followed by
approximately five minutes of GitHub cancellation cleanup and trusted
finalization. The original plan's closure is tracked separately in
[PR #1006](https://github.com/block/proto-fleet/pull/1006).

## Outcome

The trusted benchmark support landed in PR #1007 at
`c961dcba492a919a9f656e9b71b201e9b4404a25`. The initial adjudicated
[run 33774469386](https://github.com/block/proto-fleet/actions/runs/33774469386)
completed two of six cases. PRs #944, #948, #954, and #956 each exhausted the
14-minute outer reviewer budget; their finalizers verified successful setup,
that Codex reached the review phase, and 1,140–1,141 seconds of job-plus-cleanup
evidence before writing timeout artifacts. Every model case produced exactly
one trusted result or verified-timeout artifact. Completion remained 2/6 versus
the initial 12-minute `unified=40` control: PR #953 completed instead of PR
#956, so the additional two minutes yielded no net initial completion gain.

| Case | Outcome | Risk | Recall |
| --- | --- | --- | --- |
| PR #944 | Verified timeout | — | Clean control not evaluated |
| PR #948 | Verified timeout | — | Expected `MEDIUM` not evaluated |
| PR #953 | Completed at 133s | `HIGH` | Expected `HIGH` recalled; expected `MEDIUM` missed |
| PR #954 | Verified timeout | — | Two expected `MEDIUM` findings not evaluated |
| PR #956 | Verified timeout | — | Clean control not evaluated |
| PR #961 | Completed at 109s | `HIGH` | Expected `HIGH` recalled; expected `MEDIUM` missed |

The completed reviews recalled both adjudicated `HIGH` findings and neither of
the two adjudicated `MEDIUM` findings available in completed cases. PR #953
also reported a new `HIGH`: the automation start path snapshots live device
sites into the event envelope without comparing them with the response
profile's persisted authorization envelope. That finding is a valid staged-design gap rather than a newly introduced
execution regression: this persistence slice explicitly deferred automation
binding and permission enforcement, but the changed path stores live sites as
an authorization envelope that later stages must not trust as the profile's
bound authorization. The accepted curtailment scope design requires automation
OFF/start to check the exact bound
profile envelope and current principal authorization; a moved or recreated
explicit device must not silently widen the envelope at trigger time.

Completed reviews had a 121-second median wall time (109–133 seconds), reported
a median 76,409 tokens (46,891–105,926), and made a median 23 shell tool calls
(7–39). Neither log contained a byte-identical repeated shell command or a
context-compaction marker.

The candidate failed the mandatory initial 6/6 completion gate and the
completed-case medium-recall gate. Per the accepted sequence, repeats cannot
erase the initial completion failure. No repeat, large-PR run, production
rollout, or production observation was performed. Production remains at the
nine-minute outer reviewer budget with `gpt-5.6-sol`, `unified=40`, `xhigh`, and
the baseline prompt.

## Goals

- Test whether a 14-minute outer reviewer budget materially improves Sol
  completion on the fixed six-PR adjudicated corpus.
- Hold model, context, reasoning, prompt, output contract, sandbox, and trusted
  finalization constant with production.
- Preserve fail-closed timeout and automation-failure classifications.
- Bound the initial experiment to six cases, at most two concurrent model jobs,
  and at most 84 reviewer-job minutes.
- Produce uniquely named result or verified-timeout artifacts for manual
  adjudication.

## Non-goals

- Changing production before the candidate passes every gate.
- Changing `gpt-5.6-sol`, `unified=40`, `xhigh`, or the baseline prompt.
- Repeating the rejected context, effort, prompt, sharding, or Terra candidates.
- Running repeats or the large-PR corpus without another explicit billable-run
  approval.
- Weakening timeout evidence, fallback severity, or the approval-free review
  policy.

## Fixed candidate

Add a typed `codex-security-review-extended-timeout-benchmark`
`repository_dispatch` event to the trusted default-branch benchmark workflow.
It selects exactly this profile:

| Setting | Value |
| --- | --- |
| Corpus | Six adjudicated PRs |
| Model | `gpt-5.6-sol` |
| Context | `unified=40` |
| Reasoning effort | `xhigh` |
| Service tier | Unspecified, matching production |
| Verbosity | Unspecified, matching production |
| Prompt | Baseline |
| Outer reviewer-job budget, including setup | 14 minutes |
| Cancellation cleanup evidence | 5 minutes |
| Expected fail-closed ceiling | Approximately 20 minutes |
| Repeat | Initial only |

The event type selects a trusted profile; caller-controlled values cannot choose
another model, context, effort, prompt, corpus, repeat, or timeout. The selector
emits the allowlisted timeout for the outer reviewer job, Codex step metadata,
result artifact, and trusted finalizer. Because the pinned composite action
ignores caller-step cancellation, the enforceable 14-minute clock begins with
the job and includes checkout, packet generation, and setup; this experiment
does not claim 14 minutes of model processing. It compares the same boundary as
the current nine-minute production and 12-minute benchmark controls. Existing
Sol, Terra, and sharded benchmark behavior remains at its current budget.

`repository_dispatch` continues to load workflow code from `main`, so the API
key cannot be exposed to feature-branch workflow changes. Concurrency keys map
all inputs to allowlisted values before selection and serialize duplicate
extended-timeout dispatches.

## Sequence

1. Land the typed event, trusted selector, dynamic allowlisted timeout wiring,
   tests, and this plan on `main`.
2. Dispatch one initial adjudicated run with the fixed extended-timeout event.
3. Download all six trusted artifacts and action logs.
4. Adjudicate completion, expected-finding recall, new-finding validity, wall
   time, token use, tool calls, repeated reads, and compactions.
5. Record evidence in the benchmark report and this plan in a separate PR.
6. Only if every gate passes, propose a production change from a nine- to
   14-minute outer reviewer budget with an approximately 20-minute fail-closed
   ceiling.
7. If production changes, observe a fixed 30-run window and revert to nine
   minutes if the longer budget does not materially improve completion.

## Acceptance gates

1. The initial adjudicated benchmark must complete all six cases. A verified
   timeout or automation failure rejects the candidate; repeats cannot erase the
   failure.
2. Completed reviews must recall both adjudicated `HIGH` findings without
   downgrade and at least 90% of the five adjudicated `MEDIUM` findings. With
   five findings, this requires 5/5.
3. Every new `MEDIUM`, `HIGH`, or `CRITICAL` finding must be valid. Both clean
   controls must complete without an invalid material finding.
4. Every case must produce one trusted completed-result or verified-timeout
   artifact with the exact fixed SHA range and a 14-minute outer timeout budget.
5. Timeout finalizers must require successful setup, evidence that Codex reached
   the review phase, and at least 14 reviewer-job minutes plus five cleanup
   minutes. Ambiguous or early cancellation remains an automation failure.
6. Existing 12-minute Sol and Terra events and six-minute sharded jobs must
   retain their current behavior.
7. No production workflow setting changes in the benchmark-support PR.

## Production rollout gate

Passing the six-case benchmark permits a separate production proposal; it does
not silently change production. That proposal must preserve the current
fail-closed finalizer and update the outer reviewer job, timeout metadata,
tests, and operator-facing target together.

The first 30 chronological runs after rollout form the production observation
window. Report all runs, but calculate model completion among runs with trusted
completed or verified-timeout outcomes. The longer budget is retained only if:

- at least 50% of trusted model outcomes complete, compared with 3/16 (18.75%)
  in the original observation;
- every verified timeout posts an exact-SHA `HIGH` fallback within 20 minutes;
- every superseded, ambiguous, setup, API, artifact, or posting failure remains
  fail closed;
- no reviewer reaches GitHub's 30-minute ceiling; and
- human adjudication finds no quality regression or invalid approval-free path.

Packet size and rapid-push concentration must be reported so the new window is
not compared to the PR #977-heavy baseline without qualification.

## Test plan

- Execute the trusted selector and assert the exact Sol/xhigh/unified-40/baseline
  profile and 14-minute outer reviewer budget.
- Reject extended-timeout requests for another corpus, context, effort, prompt,
  or repeat.
- Assert the typed event routes only to the unsharded reviewer and finalizer.
- Assert the selected timeout controls the outer reviewer job, Codex step
  metadata, and trusted timeout classifier.
- Exercise the classifier at 14 reviewer-job minutes plus five minutes of cleanup
  and reject shorter cancellation evidence.
- Keep production/benchmark prompt, packet, schema, sandbox, and safety parity
  tests passing.
- Run the review-policy and sharding suites, Ruff, Actionlint, corpus validation,
  and `git diff --check`.

## Cost and authorization

The developer approved one six-case billable benchmark on 2026-09-03 after the
trusted support lands on `main`. With two-way concurrency and a 14-minute
budget, the run is capped at 84 reviewer-job minutes and no more than 84
model-minutes. Any repeat, large-PR run, or production rollout requires a
separate decision; secret-backed execution must never run from this feature
branch.
