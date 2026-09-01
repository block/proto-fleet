# Codex security review benchmark report

Date: 2026-09-01

## Decision

Keep the production reviewer on `unified=40`, `xhigh` reasoning effort, and the
baseline prompt. None of the context, effort, or prompt candidates met the
finding-recall and completion gates in the
[bounded-review plan](./plans/2026-08-25-bounded-codex-security-review-plan.md).
The experiment included the two planned context repeats and all three effort
levels.

The bounded execution and fail-closed finalizer remain useful independently of
model tuning. Across 72 matrix cases, 50 reviews exhausted the outer budget and
all 72 trusted finalizers uploaded either a completed-result or verified-timeout
artifact. No ambiguous cancellation was admitted as benchmark data.

Architecture-aware sharding also failed its initial gate. The trusted sharded
run completed only one of six aggregates; eight of 11 active shards exhausted
their six-minute model budget. Do not repeat the sharded candidate or run it
against the large-PR corpus. The completed
[sharded-review TDD](./plans/archive/2026-08-27-sharded-codex-security-review-tdd.md)
records the design and outcome.

The independent `gpt-5.6-terra` candidate also failed. It completed five of six
cases and recalled both adjudicated `HIGH` findings, but recalled none of the
five adjudicated `MEDIUM` findings and produced one invalid new `MEDIUM`. The
completed [Terra benchmark plan](./plans/archive/2026-09-01-terra-codex-security-review-benchmark-plan.md)
records the fixed profile and outcome. Production remains on `gpt-5.6-sol`,
`unified=40`, `xhigh`, and the baseline prompt.

## Method

All runs used the trusted `repository_dispatch` workflow from `main`. The first
three loaded main at `c126c0b6bfa0947967caa5a04474503cdff5c200`; the repeats
and medium-effort run loaded `413e4e5cd0fbc9ab3885f8ab04779456d74379a3`.
Both commits contain the same benchmark workflow blob,
`84baadfa572efbf601ff53444b4b3902cb221564`.

The fixed corpus contains two adjudicated `HIGH` findings, five adjudicated
`MEDIUM` findings, and two clean controls across PRs #944, #948, #953, #954,
#956, and #961. Each context variant ran three times. Finding recall is
calculated only across completed reviews, as required by the plan; completion
and verified-timeout rates are reported separately.

| Experiment | Run | Cases | Constant dimensions |
| --- | --- | ---: | --- |
| Context, initial | [33009198561](https://github.com/block/proto-fleet/actions/runs/33009198561) | 18 | `xhigh`, baseline prompt |
| Context, repeat 1 | [33070365755](https://github.com/block/proto-fleet/actions/runs/33070365755) | 18 | `xhigh`, baseline prompt |
| Context, repeat 2 | [33080598457](https://github.com/block/proto-fleet/actions/runs/33080598457) | 18 | `xhigh`, baseline prompt |
| Effort, `high` | [33046668278](https://github.com/block/proto-fleet/actions/runs/33046668278) | 6 | `unified=40`, baseline prompt |
| Effort, `medium` | [33092678963](https://github.com/block/proto-fleet/actions/runs/33092678963) | 6 | `unified=40`, baseline prompt |
| Prompt | [33065249002](https://github.com/block/proto-fleet/actions/runs/33065249002) | 6 | `unified=40`, `xhigh` |
| Terra | [33551271863](https://github.com/block/proto-fleet/actions/runs/33551271863) | 6 | `unified=40`, `high`, default service tier, low verbosity, baseline prompt |

GitHub reports each run as cancelled because timed-out matrix jobs are cancelled
at the enforceable outer boundary. That top-level conclusion is expected here;
every per-case finalizer succeeded and produced one uniquely named artifact.

### Sharded candidate

The initial sharded run
[33534337325](https://github.com/block/proto-fleet/actions/runs/33534337325)
loaded trusted `main` at
`bfbc12bb61dd6372ddd25c05107a1fb41c94dfec`. It used `gpt-5.6-sol`,
`unified=40`, `xhigh`, and the baseline prompt. Each review had at most two
parallel packets and each active model job had a six-minute outer budget.

| Case | Shard outcomes | Aggregate | Adjudicated result |
| --- | --- | --- | --- |
| PR #944 | timeout at 351s / timeout at 354s | Incomplete | Clean control |
| PR #948 | timeout at 353s / timeout at 352s | Incomplete | One `MEDIUM` |
| PR #953 | timeout at 355s / timeout at 355s | Incomplete | One `HIGH`, one `MEDIUM` |
| PR #954 | timeout at 354s / completed at 105s | Incomplete | Two `MEDIUM` findings |
| PR #956 | timeout at 351s / completed at 45s | Incomplete | Clean control |
| PR #961 | completed at 82s / inactive | Completed | One `HIGH`, one `MEDIUM` |

Three active shards completed and eight produced verified budget-timeout
artifacts. All trusted preparation and finalizer jobs succeeded. Each incomplete
aggregate uploaded fail-closed `HIGH` evidence and then failed its completion
gate. No timeout was reclassified as an automation failure.

PR #961's completed aggregate recalled the adjudicated `HIGH` about the shared
824-day certificate lifetime. It missed the adjudicated `MEDIUM` that existing
deployments do not receive the compatibility fix. The candidate therefore
failed both the mandatory initial 6/6 completion gate and the only recall check
available from a completed aggregate. Per the accepted sequence, repeats cannot
erase the completion failure, and PRs #957 and #964 were not run.

### Terra candidate

The initial Terra run
[33551271863](https://github.com/block/proto-fleet/actions/runs/33551271863)
loaded trusted workflow code from `main` at
`603ed457a23e9fe6c23ca8792b6aaad2e01722d1`. Each case still checked out its
fixed historical head from the corpus. The run used
`gpt-5.6-terra`, `unified=40`, `high` reasoning, explicit `default` service
tier, low verbosity, the baseline prompt, and the existing 12-minute outer
budget.

| Case | Outcome | Risk | Adjudicated result | Recall |
| --- | --- | --- | --- | --- |
| PR #944 | Verified timeout | — | Clean control | Not evaluated |
| PR #948 | Completed at 150s | `NONE` | One `MEDIUM` | Missed |
| PR #953 | Completed at 160s | `HIGH` | One `HIGH`, one `MEDIUM` | `HIGH` recalled; `MEDIUM` missed |
| PR #954 | Completed at 174s | `NONE` | Two `MEDIUM` findings | Both missed |
| PR #956 | Completed at 85s | `NONE` | Clean control | Correct |
| PR #961 | Completed at 56s | `HIGH` | One `HIGH`, one `MEDIUM` | `HIGH` recalled; `MEDIUM` missed |

The five completed cases had a 150-second median wall time (56–174 seconds) and
reported a median 102,377 tokens (42,354–192,784). PR #944 reached a verified
outer-budget timeout; every trusted finalizer succeeded.

Terra recalled 2/2 adjudicated `HIGH` findings and 0/5 adjudicated `MEDIUM`
findings. Its additional PR #953 `MEDIUM` claimed legacy site-scoped profile
requests reach the envelope builder with empty scope JSON. That finding is
invalid: [`validateAndNormalize`](https://github.com/block/proto-fleet/blob/586901c2c4b2ca21057202bac398718204ea6435/server/internal/domain/curtailment/response_profile.go#L220-L276)
resolves the legacy `SiteID`, marshals the effective scope, and assigns canonical
non-empty `ScopeJSON` before calling the store. That link intentionally targets
PR #953's benchmarked historical head; `603ed457a` identifies the trusted
workflow revision, not the code under review. The candidate therefore failed
completion, recall, and new-finding
validity. Repeats cannot repair the initial 6/6 failure, so PRs #957 and #964
were not run.

## Human adjudication

- Every completed PR #944 and PR #956 clean-control review correctly returned
  `NONE`.
- Every completed PR #961 review found only the certificate-expiry issue. The
  `unified=40` and `unified-10` outputs rated it `HIGH`; compact context rated it
  `MEDIUM`, `MEDIUM`, and `HIGH` across its three trials. All missed the
  existing-deployment compatibility `MEDIUM`.
- The medium-effort PR #948 review missed the adjudicated topology-preview
  `MEDIUM`. Its different `HIGH` about enabled automations retaining
  non-executable profiles was adjudicated invalid. At merged commit
  `e37f97c1c41004667ff9bc467a98cc130736a012`, the
  [domain service](https://github.com/block/proto-fleet/blob/e37f97c1c41004667ff9bc467a98cc130736a012/server/internal/domain/curtailment/response_profile.go#L135)
  and [SQL store](https://github.com/block/proto-fleet/blob/e37f97c1c41004667ff9bc467a98cc130736a012/server/internal/domain/stores/sqlstores/curtailment.go#L246)
  already reject this transition, an advisory transaction lock serializes it
  against automation changes, and unit and integration tests cover the case.
- The medium-effort PR #954 review recalled the abandoned-staging-database
  `MEDIUM` and missed the connection-retry/deadline `MEDIUM`.
- PR #953, which contains one expected `HIGH` and one expected `MEDIUM`, timed
  out in every experiment.
- The bounded prompt downgraded PR #961's expected `HIGH` to `MEDIUM` and missed
  its expected `MEDIUM`.
- Terra recalled the expected `HIGH` findings on PR #953 and PR #961. It missed
  PR #948's topology-preview `MEDIUM`, PR #953's conflicting-lock-order
  `MEDIUM`, both PR #954 database-harness `MEDIUM` findings, and PR #961's
  existing-deployment compatibility `MEDIUM`.
- Terra's new PR #953 legacy-site-profile `MEDIUM` is invalid. At the reviewed
  head, the response-profile service resolves `SiteID` through
  `ResponseProfileScope`, marshals it to canonical scope JSON, and replaces
  `profile.ScopeJSON` before the SQL store invokes the envelope builder.

## Results

### Context experiment

Recall denominators include expected findings only in completed reviews. For
example, PR #953's repeated timeouts reduce completion but do not enter recall.

| Variant | Completed | Verified timeouts | `HIGH` recall | `MEDIUM` recall | Completed clean controls | Result |
| --- | ---: | ---: | ---: | ---: | --- | --- |
| `unified-40` | 4/18 | 14 | 3/3 | 0/3 | 1/6: `NONE` | Retain as control only |
| `unified-10` | 4/18 | 14 | 2/2 | 0/2 | 2/6: `NONE` | Reject: insufficient completion and medium recall |
| compact | 5/18 | 13 | 1/3 | 0/3 | 2/6: `NONE` | Reject: two `HIGH` downgrades |

The repeats confirmed substantial model variance but did not rescue a context
candidate. For PR #956, `unified-10` reduced the packet from 812,319 to 359,607
bytes, yet its completed trials took 159 and 163 seconds; the completed
`unified-40` control took 124 seconds. For PR #961, compact context reduced the
packet from 2,347 to 849 bytes, but it downgraded the expected `HIGH` in two of
three trials and missed the expected `MEDIUM` in all three.

### Effort and prompt experiments

The `xhigh` baseline row includes all three `unified=40` context trials. The
other candidates ran once across the six-case corpus.

| Context | Effort | Prompt | Completed | Verified timeouts | `HIGH` recall | `MEDIUM` recall | Result |
| --- | --- | --- | ---: | ---: | ---: | ---: | --- |
| `unified-40` | `xhigh` | baseline | 4/18 | 14 | 3/3 | 0/3 | Production control; did not pass tuning gate |
| `unified-40` | `high` | baseline | 2/6 | 4 | 1/1 | 0/1 | Reject: no completion improvement |
| `unified-40` | `medium` | baseline | 5/6 | 1 | 1/1 | 1/4 | Reject: medium recall and validity failed |
| `unified-40` | `xhigh` | bounded | 2/6 | 4 | 0/1 | 0/1 | Reject: downgraded an adjudicated `HIGH` |

`medium` materially improved completion, but it recalled only 25% of expected
`MEDIUM` findings in completed reviews and introduced an invalid `HIGH` on PR
#948. PR #953's timeout remains a separate completion failure. Lower effort is
therefore not safe to roll out despite its runtime improvement.

### Completed-run metrics

Metrics come from result artifacts and Codex action logs. Wall time, tool calls,
and tokens show the median and range among completed cases. “Repeat commands”
counts byte-identical repeated shell commands. No context-compaction marker was
observed in any completed log.

| Run | Completed | Wall time | Tool calls | Reported tokens | Repeat commands | Compactions |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| Context initial | 4 | 113s (91–159) | 15 (4–20) | 55,442 (36,310–75,713) | 2 | 0 |
| Context repeat 1 | 5 | 98s (94–163) | 8 (7–23) | 60,142 (52,512–97,852) | 0 | 0 |
| Context repeat 2 | 4 | 124s (99–172) | 20 (8–23) | 79,708 (56,893–110,174) | 2 | 0 |
| `high` effort | 2 | 100s (83–116) | 13 (7–18) | 67,086 (53,508–80,664) | 0 | 0 |
| `medium` effort | 5 | 115s (46–130) | 12 (5–21) | 67,524 (34,043–111,720) | 0 | 0 |
| Bounded prompt | 2 | 113s (90–136) | 14 (7–20) | 50,586 (36,375–64,796) | 0 | 0 |

These timings describe only the 22 cases that completed. They are not a
representative latency distribution because the other 50 cases reached the
12-minute model budget and then spent approximately five minutes in Actions
cancellation cleanup.

## Gate evaluation

| Gate | Outcome |
| --- | --- |
| Initial candidate completion | Failed: sharding completed 1/6 aggregates; Terra completed 5/6 cases |
| Retain every adjudicated `HIGH` without downgrade across completed reviews | Terra passed at 2/2; compact context and the bounded prompt failed |
| Retain at least 90% of adjudicated `MEDIUM` findings across completed reviews | Failed by every tested configuration; Terra recalled 0/5, and the best observed result was 1/4 at `medium` effort |
| No invalid `HIGH` on completed clean controls | Passed; every completed PR #944 and PR #956 control returned `NONE` |
| New medium-or-higher findings are valid | Failed: medium effort produced an invalid `HIGH` on PR #948, and Terra produced an invalid `MEDIUM` on PR #953 |
| No increase in median tool calls or compactions | Not decision-bearing because completion and recall failed |
| At least 20% faster or equivalent with lower token use | Terra improved completion, and `medium` effort also improved completion, but both failed recall and validity gates |

The planned repeats showed that compact severity varies, but two completed
trials still downgraded an adjudicated `HIGH`. Testing `medium` closed the effort
matrix and showed that its completion gain comes with unacceptable recall.

## Follow-up

1. Keep production on `gpt-5.6-sol`, `unified=40`, `xhigh`, and the baseline
   prompt while retaining the bounded timeout and fail-closed human-review path.
2. Do not repeat the rejected sharded or Terra candidates, and do not replay PRs
   #957 and #964 for either candidate.
3. Complete the original plan's first-30-production-run observation window.
4. Require any future candidate to use trusted default-branch benchmark code and
   pass the same initial completion, recall, and finding-validity gates before
   production evaluation.
