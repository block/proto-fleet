# Codex security review benchmark report

Date: 2026-08-27

## Decision

Keep the production reviewer on `unified=40`, `xhigh` reasoning effort, and the
baseline prompt. None of the context, effort, or prompt candidates met the
finding-recall and completion gates in the
[bounded-review plan](./plans/2026-08-25-bounded-codex-security-review-plan.md).
Do not test `medium` effort or the large-PR corpus: `high` already failed the
adjudicated corpus, and the plan gates large-PR replay on selecting a candidate.

The bounded execution and fail-closed finalizer remain useful independently of
model tuning. Across 30 matrix cases, 22 reviews exhausted the outer budget and
all 30 trusted finalizers uploaded either a completed-result or verified-timeout
artifact. No ambiguous cancellation was admitted as benchmark data.

Further runtime work should evaluate architecture-aware sharding rather than
reducing context, effort, or prompt scope again. See the
[sharded-review TDD](./plans/2026-08-27-sharded-codex-security-review-tdd.md).

## Method

All runs used the trusted `repository_dispatch` workflow from `main` at
`c126c0b6bfa0947967caa5a04474503cdff5c200`. The fixed corpus contains two
adjudicated `HIGH` findings, five adjudicated `MEDIUM` findings, and two clean
controls across PRs #944, #948, #953, #954, #956, and #961. A timeout counts as
no recalled finding because it produces no usable review.

| Experiment | Run | Cases | Constant dimensions |
| --- | --- | ---: | --- |
| Context | [33009198561](https://github.com/block/proto-fleet/actions/runs/33009198561) | 18 | `xhigh`, baseline prompt |
| Effort | [33046668278](https://github.com/block/proto-fleet/actions/runs/33046668278) | 6 | `unified=40`, baseline prompt |
| Prompt | [33065249002](https://github.com/block/proto-fleet/actions/runs/33065249002) | 6 | `unified=40`, `xhigh` |

GitHub reports each run as cancelled because timed-out matrix jobs are cancelled
at the enforceable outer boundary. That top-level conclusion is expected here;
every per-case finalizer succeeded and produced one uniquely named artifact.

## Human adjudication

- The two completed PR #956 control reviews in the context run correctly
  returned `NONE`.
- The PR #961 `unified=40` control recalled the adjudicated `HIGH` certificate
  lifetime finding and missed the other adjudicated `MEDIUM` finding.
- The compact PR #961 result described the same certificate lifetime issue as
  `MEDIUM`, which is a downgrade of the adjudicated `HIGH`, and also missed the
  other adjudicated `MEDIUM`.
- Later PR #961 effort and prompt outputs were compared against the same
  adjudicated findings: `high` retained only the `HIGH`; the bounded prompt
  downgraded it to `MEDIUM`; both missed the other `MEDIUM`.

## Results

### Context experiment

| Variant | Completed | Verified timeouts | `HIGH` recall | `MEDIUM` recall | Completed clean controls | Result |
| --- | ---: | ---: | ---: | ---: | --- | --- |
| `unified-40` | 2/6 | 4 | 1/2 | 0/5 | PR #956: `NONE` | Retain as control only |
| `unified-10` | 1/6 | 5 | 0/2 | 0/5 | PR #956: `NONE` | Reject: insufficient completion and recall |
| compact | 1/6 | 5 | 0/2 | 0/5 | None completed | Reject: downgraded an adjudicated `HIGH` |

The smaller packet did not reliably improve completion. For PR #956,
`unified-10` reduced the packet from 812,319 to 359,607 bytes but took 159
seconds versus 124 seconds for `unified-40`. For PR #961, compact context reduced
the packet from 2,347 to 849 bytes and completed 11 seconds faster, but it
introduced a disqualifying severity downgrade.

### Effort and prompt experiments

| Context | Effort | Prompt | Completed | Verified timeouts | `HIGH` recall | `MEDIUM` recall | Result |
| --- | --- | --- | ---: | ---: | ---: | ---: | --- |
| `unified-40` | `xhigh` | baseline | 2/6 | 4 | 1/2 | 0/5 | Production control; did not pass tuning gate |
| `unified-40` | `high` | baseline | 2/6 | 4 | 1/2 | 0/5 | Reject; do not test `medium` |
| `unified-40` | `xhigh` | bounded | 2/6 | 4 | 0/2 | 0/5 | Reject; downgraded an adjudicated `HIGH` |

Neither lower effort nor budget guidance changed which cases completed. The
bounded prompt also regressed severity on the only completed finding-bearing
case.

### Completed-run metrics

Metrics below come from the result artifacts and the Codex action logs. “Repeat”
counts byte-identical repeated shell commands; no context-compaction marker was
observed in any completed log.

| Experiment | Case / variant | Wall time | Tool calls | Reported tokens | Repeat | Compactions | Risk |
| --- | --- | ---: | ---: | ---: | ---: | ---: | --- |
| Context | PR #956 / `unified-40` | 124s | 20 | 67,165 | 0 | 0 | `NONE` |
| Context | PR #956 / `unified-10` | 159s | 17 | 75,713 | 2 | 0 | `NONE` |
| Context | PR #961 / `unified-40` | 102s | 13 | 43,719 | 0 | 0 | `HIGH` |
| Context | PR #961 / compact | 91s | 4 | 36,310 | 0 | 0 | `MEDIUM` |
| Effort | PR #956 / `unified-40` | 116s | 18 | 80,664 | 0 | 0 | `NONE` |
| Effort | PR #961 / `unified-40` | 83s | 7 | 53,508 | 0 | 0 | `HIGH` |
| Prompt | PR #956 / `unified-40` | 136s | 20 | 64,796 | 0 | 0 | `NONE` |
| Prompt | PR #961 / `unified-40` | 90s | 7 | 36,375 | 0 | 0 | `MEDIUM` |

These timings describe only the eight cases that completed. They are not a
representative latency distribution because the other 22 cases reached the
12-minute model budget and then spent approximately five minutes in Actions
cancellation cleanup.

## Gate evaluation

| Gate | Outcome |
| --- | --- |
| Retain every adjudicated `HIGH` without downgrade | Failed by compact context and bounded prompt |
| Retain at least 90% of adjudicated `MEDIUM` findings | Failed by every tested configuration |
| No invalid `HIGH` on completed clean controls | Passed for completed PR #956 controls; PR #944 did not complete |
| No increase in median tool calls or compactions | Not decision-bearing because recall failed |
| At least 20% faster or equivalent with lower token use | Not decision-bearing because recall failed |

Additional repeats cannot make the compact or bounded candidates satisfy the
“every `HIGH`” gate because each already produced a completed severity
downgrade. The `high` effort candidate also produced no completion improvement
and missed the only adjudicated `MEDIUM` in its completed finding-bearing case.

## Follow-up

1. Keep production tuning unchanged while retaining the bounded timeout and
   fail-closed human-review path.
2. Complete the original plan's first-30-production-run observation window.
3. Review the sharded-review TDD before implementing another secret-backed
   benchmark candidate.
4. Require a sharded candidate to pass the same adjudicated recall gates before
   replaying PRs #957 and #964 from the large-PR corpus.
