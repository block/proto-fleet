# Codex security review benchmark report

Date: 2026-08-27

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

Further runtime work should evaluate architecture-aware sharding rather than
reducing context, effort, or prompt scope again. See the
[sharded-review TDD](./plans/2026-08-27-sharded-codex-security-review-tdd.md).

## Method

All runs used the trusted `repository_dispatch` workflow from `main`. The first
three loaded main at `c126c0b6bfa0947967caa5a04474503cdff5c200`; the repeats
and medium-effort run loaded `413e4e5cd0fbc9ab3885f8ab04779456d74379a3`.
Both commits contain the same benchmark workflow blob,
`84baadfa572efbf601ff53444b4b3902cb221564`.

The fixed corpus contains two adjudicated `HIGH` findings, five adjudicated
`MEDIUM` findings, and two clean controls across PRs #944, #948, #953, #954,
#956, and #961. Each context variant ran three times. A timeout counts as no
recalled finding because it produces no usable review.

| Experiment | Run | Cases | Constant dimensions |
| --- | --- | ---: | --- |
| Context, initial | [33009198561](https://github.com/block/proto-fleet/actions/runs/33009198561) | 18 | `xhigh`, baseline prompt |
| Context, repeat 1 | [33070365755](https://github.com/block/proto-fleet/actions/runs/33070365755) | 18 | `xhigh`, baseline prompt |
| Context, repeat 2 | [33080598457](https://github.com/block/proto-fleet/actions/runs/33080598457) | 18 | `xhigh`, baseline prompt |
| Effort, `high` | [33046668278](https://github.com/block/proto-fleet/actions/runs/33046668278) | 6 | `unified=40`, baseline prompt |
| Effort, `medium` | [33092678963](https://github.com/block/proto-fleet/actions/runs/33092678963) | 6 | `unified=40`, baseline prompt |
| Prompt | [33065249002](https://github.com/block/proto-fleet/actions/runs/33065249002) | 6 | `unified=40`, `xhigh` |

GitHub reports each run as cancelled because timed-out matrix jobs are cancelled
at the enforceable outer boundary. That top-level conclusion is expected here;
every per-case finalizer succeeded and produced one uniquely named artifact.

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

## Results

### Context experiment

The recall denominators include all three trials: six expected `HIGH`
observations and 15 expected `MEDIUM` observations per variant.

| Variant | Completed | Verified timeouts | `HIGH` recall | `MEDIUM` recall | Completed clean controls | Result |
| --- | ---: | ---: | ---: | ---: | --- | --- |
| `unified-40` | 4/18 | 14 | 3/6 | 0/15 | 1/6: `NONE` | Retain as control only |
| `unified-10` | 4/18 | 14 | 2/6 | 0/15 | 2/6: `NONE` | Reject: insufficient completion and recall |
| compact | 5/18 | 13 | 1/6 | 0/15 | 2/6: `NONE` | Reject: two `HIGH` downgrades |

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
| `unified-40` | `xhigh` | baseline | 4/18 | 14 | 3/6 | 0/15 | Production control; did not pass tuning gate |
| `unified-40` | `high` | baseline | 2/6 | 4 | 1/2 | 0/5 | Reject: no completion improvement |
| `unified-40` | `medium` | baseline | 5/6 | 1 | 1/2 | 1/5 | Reject: recall below both gates |
| `unified-40` | `xhigh` | bounded | 2/6 | 4 | 0/2 | 0/5 | Reject: downgraded an adjudicated `HIGH` |

`medium` materially improved completion, but it recalled only 20% of expected
`MEDIUM` findings, missed the `HIGH` in timed-out PR #953, and introduced an
invalid `HIGH` on PR #948. Lower effort is therefore not safe to roll out
despite its runtime improvement.

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
| Retain every adjudicated `HIGH` without downgrade | Failed by compact context and bounded prompt; other candidates timed out on PR #953 |
| Retain at least 90% of adjudicated `MEDIUM` findings | Failed by every tested configuration; best observed recall was 1/5 at `medium` |
| No invalid `HIGH` on completed clean controls | Passed; every completed PR #944 and PR #956 control returned `NONE` |
| New medium-or-higher findings are valid | Failed; medium effort produced an invalid `HIGH` on PR #948 |
| No increase in median tool calls or compactions | Not decision-bearing because recall failed |
| At least 20% faster or equivalent with lower token use | `medium` improved completion, but recall failed |

The planned repeats showed that compact severity varies, but two completed
trials still downgraded an adjudicated `HIGH`. Testing `medium` closed the effort
matrix and showed that its completion gain comes with unacceptable recall.

## Follow-up

1. Keep production tuning unchanged while retaining the bounded timeout and
   fail-closed human-review path.
2. Complete the original plan's first-30-production-run observation window.
3. Review the sharded-review TDD before implementing another secret-backed
   benchmark candidate.
4. Require a sharded candidate to pass the same adjudicated recall gates before
   replaying PRs #957 and #964 from the large-PR corpus.
