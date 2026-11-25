# Codex security review benchmark

The benchmark replays fixed historical PR ranges to compare Astra and Sol
using the production security-review prompt. It produces evidence for human
adjudication; it does not post PR comments, approve PRs, or change production
review policy.

## Production reviewer trial

The supplementary production reviewer uses `gpt-6.1-sol` at `medium` reasoning
effort. Execution, finalized result artifacts, and the posted comment inherit one
workflow-level model setting. The prompt, sandbox, output validation, timeout
budget, trusted finalizer, and human-review policy retain their existing behavior.

The 2026-11-11 paired validation runs used the same trusted workflow revision
`6f3775f2eed3a2d73e495edf1aae7012015ff45f`. They ran in the internal repository
before the repository consolidation, so their run pages are not public:
[Astra](https://github.com/btc-mining/proto-fleet-internal/actions/runs/43078247871)
and [Sol](https://github.com/btc-mining/proto-fleet-internal/actions/runs/43078250039).
Both completed 6/6 cases and kept both clean controls clean. Both detected the
frozen #1077 finding and missed the other four required findings, including the
two migration findings in #1090. Their additional #1090 findings do not count
as frozen recall. Neither passed the frozen validation gate.

Sol had a lower median recorded case time (58.5s versus 76.5s) and lower total
recorded case time (560s versus 583s). This supports a reversible trial, not a
claim of proven quality improvement. Preserve the frozen expectations and
observe the first 30 chronological production runs as described below before
assessing the trial; the historical promotion gates remain the quality target.

## Profiles and scope

| Profile | Model | Reasoning | Outer reviewer budget |
| --- | --- | --- | --- |
| `astra` | `gpt-6-astra` | `medium` | 30 minutes |
| `sol` | `gpt-6.1-sol` | `medium` | 30 minutes |

All profiles use the production prompt, `unified=40`, no inter-hunk context,
the default service tier and verbosity, read-only sandbox, `drop-sudo`, and the
same structured output contract. Tests enforce parity with production. The job
budget includes checkout and setup; cancellation can add approximately five
minutes before trusted finalization. Early action failures, setup failures, and
early or ambiguous cancellations fail the workflow as automation failures; they
do not produce model-completion evidence.

The checked-in [corpus](../../.github/codex-benchmark-corpus.json) pins the source
repository, `block/proto-fleet`, and exact base/head SHAs. The reviewer checks
out those pinned commits. The selector, inline workflow code, finalizer, and
artifacts belong to the dispatched workflow run. `adjudicated` selects six cases; `large-pr` selects two
historical timeouts. `validation` selects six fixed cases whose expectations
were frozen in [commit 47d320db5](https://github.com/block/proto-fleet/commit/47d320db5c82afa75286d86f96edf3693523f39d)
before the Astra/Sol medium comparison. Neither the model, source repository,
nor commit ranges can be supplied by the caller. Historical checkouts fetch
only the pinned commits and their ancestors, excluding later fixes from the
workspace Git history.
The retired context, effort-sweep, bounded-prompt, Terra, sharding, Sol high, and
extended-timeout experiments are available in Git history, not as dispatch
options.

## Running a comparison

The workflow must first land on the repository's default branch. Only
`repository_dispatch` is supported: feature-branch workflow code cannot receive
the benchmark API key through a `workflow_dispatch --ref` invocation.
The repository needs `OPENAI_API_KEY` with access to both models.

Each dispatch is billable. One adjudicated profile runs six reviewers, at most
two concurrently, with an upper bound of 180 reviewer-job minutes, excluding
cancellation cleanup and finalizers. An initial paired comparison needs two
dispatches and up to 360 reviewer-job minutes. Different profiles can run
concurrently. These time limits are not a dollar or token cap.

For the initial model comparison, submit this request with `profile` set to
`astra`, then submit the same request with `profile` set to `sol`, after approval
for those specific runs:

```json
{
  "event_type": "codex-security-review-benchmark",
  "client_payload": {
    "profile": "astra",
    "corpus": "adjudicated",
    "repeat": "initial"
  }
}
```

Save it as a JSON file and send it to `block/proto-fleet`, which contains the
trusted workflow:

```sh
gh api --method POST repos/block/proto-fleet/dispatches --input /tmp/benchmark-request.json
```

`profile` is required. `corpus` defaults to `adjudicated`; `repeat` defaults to
`initial`. Allowed repeats are `initial`, `repeat-1`, and `repeat-2`; they label
independent measurements and do not schedule retries. Unknown fields and
invalid values fail selection before model jobs start. Matching dispatches are
serialized; submitting a duplicate can still incur another run.

Run both profiles from the same trusted workflow revision. Record that revision,
run IDs, run attempts, profile, and corpus with the results. Launch repeats or
large-PR runs only after the initial comparison is adjudicated and those runs
are approved. Automation failures invalidate the trial: fix the configuration
or infrastructure and rerun the same initial comparison. A valid trial that
fails the initial model-completion gate cannot be erased by repeats.

For a comparison across workflow revisions, verify that the model's inputs and
all settings except the intended experimental dimension match. Settle disputed
expected findings and record the scoring rubric before evaluating a new
candidate; apply any rubric correction consistently to every compared profile.
Keep unadjudicated new findings separate from known-finding recall.

## Evidence and adjudication

Download every per-case artifact before it expires. Each contains
`benchmark-review.json`, `benchmark-review.md`, `benchmark-scope.json`, and
`benchmark-elapsed-time.txt`. Scope records the source repository, model, exact
commit range, configuration, packet metrics, completion, and incomplete reason.
Verified job timeouts have separate `benchmark-timeout-*` artifacts and unknown
elapsed time; treat these as incomplete reviews, not detected findings.

Keep the action logs as well: token usage, tool calls, repeated reads, and
compactions require log inspection. Record input/cached/output usage separately
when available and the pricing date used for cost calculations. If the logs do
not expose that breakdown, mark cost unavailable rather than estimating it from
an undifferentiated token total. Compare elapsed time and usage on matched
completed cases, and report completion and timeout rates separately.

Use this re-adjudicated finding rubric for the six fixed historical heads.
Historical review comments are evidence to verify, not ground truth. The source
runs and comments are linked from each corpus entry. The original comparison
outputs were rescored against the same deduplicated set before selecting the
separate validation corpus; this expanded set is retrospective evidence.

| Case | Expected material findings |
| --- | --- |
| #944 | Clean authentication-change control; `NONE` |
| #948 | Two `HIGH`: management profile adapter loses topology scope; clearing the last topology target selects the whole organization. Two `MEDIUM`: unchanged ancestor picker broadens the target; topology/all-paired profile saves are rejected by the backend |
| #953 | `HIGH`: migration rejects databases containing curtailment history without a preserving upgrade path. Two `MEDIUM`: conflicting device-row lock orders; profile fan/topology locks invert existing rack-update lock order |
| #954 | Two `MEDIUM`: abandoned staging-database cleanup and connection retry/deadline handling |
| #956 | Clean dependency-change control; `NONE` |
| #961 | `HIGH`: shared 824-day CA/internal/service certificate expiry without a supported renewal path at the reviewed head |

The old #948 saved-policy preview claim cannot assume persistence: the reviewed
backend rejects that topology/all-paired combination. Score the reachable save
failure instead. The old #961 existing-installation expectation describes
pre-existing rollout behavior rather than an introduced defect. Neither remains
a required finding. Scope-broadening findings require subsequent operator
actions; Run confirmation names the whole fleet. Migration rejection is
conditional on existing rows, and certificate failure on reaching expiry
without replacement. In #954, later context-aware operations prevent late
publication; the deadline/resource and unswept-staging defects remain, with
conditional rather than guaranteed package-timeout impact.

Judge each finding against the fixed historical head, including relevant
unchanged context. Do not use the current main branch to validate an old finding.
Every new material finding needs independent human adjudication; matching only
the overall risk is insufficient. Report per-finding recall and severity,
invalid findings, clean-control results, completion, and automation failures.

For `validation`, use the frozen expectations in commit 47d320db5: both `HIGH`
findings in #1090, one `MEDIUM` each in #1076, #1077, and #1080, and clean
controls #1095 and #1102. Require 6/6 completed cases, no required finding
missed or downgraded, both controls clean, and no invalid new
`MEDIUM`-or-higher finding. Keep unexpected findings separate from frozen
recall. Passing validation does not itself promote a model.

The two validation clean controls already returned `NONE` in Astra reviews
of these exact ranges, linked from the corpus. Report that prior exposure
with the results: they provide new clean-control evidence for Sol, while
Astra's results measure repeatability on previously reviewed inputs. Do not
count them as unseen clean-control validation for Astra.

## Promotion gates

The production trial above is a deliberate, reversible exception to these
historical promotion gates. Neither model passed frozen validation; this trial
does not count as passing these gates or establish a quality improvement.
Use the trial decision rule below for this exception, while retaining these
gates as the quality target for future evaluation.

The initial Sol candidate must complete 6/6 cases, retain all four adjudicated
`HIGH` findings without downgrade, retain all six adjudicated `MEDIUM`
findings, complete both clean controls without invalid material findings, and
introduce no invalid new `MEDIUM`-or-higher finding. Verified model timeouts
count against initial completion. Automation failures invalidate the trial and
require correction and rerun before judging the candidate. Also report Astra's
quality against the same absolute bar; Astra is a comparison control, not
assumed ground truth.

Passing the initial gate permits further evaluation. Repeat the matched
comparison to assess variance, review the large-PR corpus and representative
recent PRs, and compare actual cost and latency alongside quality. Use fresh
fixed-SHA cases with human adjudication when extending the corpus. Change
reasoning effort only in a separate experiment.

Production promotion is a separate change. Preserve its trusted finalizer and
human-review path, and update execution, validation, result metadata, comment
model identity, and tests together. Observe the first 30 chronological runs
after promotion, reporting completion, verified timeouts, automation failures,
latency, usage, packet sizes, and human finding dispositions.

### Trial decision after 30 runs

Compare the first 30 chronological Sol production runs with the last 30
chronological Astra production runs before the switch. Include every eligible
reviewer run, including incomplete reviews and automation failures; list run
attempts and repeated reviews of the same PR separately rather than selecting
only successful or favorable cases. Identify Sol runs from `model` in
`codex-security-review-result.json`. Older Astra artifacts lack that field:
verify their model from the workflow revision and action logs, and record any
unavailable evidence explicitly.

Human reviewers must adjudicate material automated findings and material
findings the automation missed on those exact PR heads for both samples.
Report the counts and dispositions alongside completion, timeout and automation
failure rates, latency, and packet sizes. These are different PR workloads;
the comparison is observational and cannot prove equal quality or a model-only
latency improvement.

Revert early if independent human adjudication confirms an invalid
`MEDIUM`-or-higher Sol finding or a missed `HIGH`/`CRITICAL` finding in a trial
PR. At run 30, keep Sol only after a human accepts a report showing no such
quality failures, a completion rate at least as high as the Astra sample, and
a lower median latency on completed reviews. Missing artifacts or incomplete
human adjudication make the result inconclusive: revert to Astra at the end
of the trial unless a separate change explicitly approves further evaluation.
Passing this trial rule keeps the supplementary reviewer on Sol; it does not
waive the historical promotion gates or the human-review requirement.
