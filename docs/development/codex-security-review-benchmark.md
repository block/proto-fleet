# Codex security review benchmark

The benchmark replays fixed historical PR ranges to compare the production
security reviewer with GPT-6.1 Sol. It produces evidence for human adjudication;
it does not post PR comments, approve PRs, or change production review policy.

## Profiles and scope

| Profile | Model | Reasoning | Outer reviewer budget |
| --- | --- | --- | --- |
| `astra` | `gpt-6-astra` | `medium` | 30 minutes |
| `sol` | `gpt-6.1-sol` | `medium` | 30 minutes |
| `sol-high` | `gpt-6.1-sol` | `high` | 30 minutes |

All profiles use the production prompt, `unified=40`, no inter-hunk context,
the default service tier and verbosity, read-only sandbox, `drop-sudo`, and the
same structured output contract. Tests enforce parity with production. The job
budget includes checkout and setup; cancellation can add approximately five
minutes before trusted finalization. Early action failures, setup failures, and
early or ambiguous cancellations fail the workflow as automation failures; they
do not produce model-completion evidence.

The checked-in [corpus](../../.github/codex-benchmark-corpus.json) pins the source
repository, `block/proto-fleet`, and exact base/head SHAs. These historical
commits belong to upstream and are not available through the internal
repository's commits API. The reviewer explicitly checks out upstream, while
the selector, inline workflow code, finalizer, and artifacts remain in the
internal workflow run. `adjudicated` selects six cases; `large-pr` selects two
historical timeouts. Neither the model, source repository, nor commit ranges
can be supplied by the caller.
The retired context, effort-sweep, bounded-prompt, Terra, sharding, and
extended-timeout experiments are available in Git history, not as dispatch
options. `sol-high` is the only dispatchable effort variant.

## Running a comparison

The workflow must first land on the repository's default branch. Only
`repository_dispatch` is supported: feature-branch workflow code cannot receive
the benchmark API key through a `workflow_dispatch --ref` invocation.
The repository needs `OPENAI_API_KEY` with access to both models.

Each dispatch is billable. One adjudicated profile runs six reviewers, at most
two concurrently, with an upper bound of 180 reviewer-job minutes, excluding
cancellation cleanup and finalizers. An initial paired comparison needs two
dispatches and up to 360 reviewer-job minutes. Different profiles can run
concurrently. Run matched `sol` and `sol-high` comparisons sequentially. They
share a model and API key, so overlapping runs could trigger rate-limit backoff
that skews latency or timeout results. These time limits are not a dollar or
token cap.

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

To evaluate higher reasoning effort, dispatch one initial `sol-high` run on the
same adjudicated corpus. It changes only reasoning effort relative to `sol`;
retain the same prompt, packet construction, and 30-minute budget. Compare its
completion and verified-timeout rate, finding recall, validity, latency, and
usage with the `sol` control. `sol-high` must independently meet the initial
promotion gates below before production promotion is considered. Do not rerun
the model comparison merely to start the effort experiment.

Save it as a JSON file and send it to the internal repository containing the
trusted workflow. The dispatch target and historical checkout source are
separate: dispatch here, then the reviewer fetches `block/proto-fleet`:

```sh
gh api --method POST repos/btc-mining/proto-fleet-internal/dispatches --input /tmp/benchmark-request.json
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

Use this finding rubric for the six fixed historical heads. The source runs
and comments are linked from each corpus entry.

| Case | Expected material findings |
| --- | --- |
| #944 | Clean authentication-change control; `NONE` |
| #948 | `MEDIUM`: topology preview can diverge across the client/domain boundary |
| #953 | `HIGH`: migration rejects databases containing curtailment history; `MEDIUM`: conflicting device-row lock orders can deadlock profile saves |
| #954 | Two `MEDIUM`: abandoned staging-database cleanup and connection retry/deadline handling |
| #956 | Clean dependency-change control; `NONE` |
| #961 | `HIGH`: shared 824-day certificate lifetime; `MEDIUM`: existing deployments do not receive the compatibility fix |

Judge each finding against the fixed historical head, including relevant
unchanged context. Do not use the current main branch to validate an old finding.
Every new material finding needs independent human adjudication; matching only
the overall risk is insufficient. Report per-finding recall and severity,
invalid findings, clean-control results, completion, and automation failures.

## Promotion gates

The initial Sol candidate must complete 6/6 cases, retain both adjudicated
`HIGH` findings without downgrade, retain all five adjudicated `MEDIUM`
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
latency, usage, packet sizes, and human finding dispositions. Revert the model
change if that observation shows a quality regression.
