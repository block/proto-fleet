---
title: "Terra Codex security review benchmark"
date: 2026-09-01
status: implementing
type: plan
tracker: https://github.com/block/proto-fleet/pull/987
---

# Terra Codex security review benchmark

## Context

The bounded Sol benchmark completed 22 of 72 matrix cases, and the initial
architecture-aware sharding candidate completed only one of six aggregates.
Neither runtime approach passed its completion and recall gates. Production
therefore remains on `gpt-5.6-sol`, `unified=40`, `xhigh`, and the baseline
prompt.

OpenAI's current model documentation describes `gpt-5.6-terra` as balancing
quality, latency, and cost. The current Codex configuration schema supports
`model_reasoning_effort`, `service_tier`, and `model_verbosity`; the pinned
Codex action accepts these as trusted `codex-args` overrides. This experiment
tests Terra independently so a model change is not mixed with sharding or
prompt changes.

## Goals

- Measure Terra completion and finding recall against the fixed adjudicated
  corpus.
- Hold diff context, prompt, output schema, sandbox, timeout, and trusted
  finalization constant with the retained Sol control.
- Bind Terra to `high` reasoning, explicit `default` service tier, and `low`
  verbosity through trusted default-branch code.
- Preserve uniquely named completed-result and verified-timeout artifacts for
  manual adjudication.

## Non-goals

- Changing the production review model or settings.
- Repeating the rejected sharding candidate.
- Allowing repository-dispatch payloads to choose arbitrary models, service
  tiers, or verbosity values.
- Running the large-PR corpus before the adjudicated gate passes.

## Design

Add the typed `codex-security-review-terra-benchmark` repository-dispatch event
to the existing trusted benchmark workflow. The event maps to one fixed profile:

| Setting | Value |
| --- | --- |
| Model | `gpt-5.6-terra` |
| Context | `unified=40` |
| Reasoning effort | `high` |
| Service tier | `default` |
| Verbosity | `low` |
| Prompt | Baseline |
| Outer model budget | 12 minutes |

The event type, not caller-controlled profile data, selects the model and Codex
arguments. The trusted selector rejects a Terra dispatch that requests another
context, effort, or prompt. Concurrency keys canonicalize every payload value to
an allowed option before selection, so malformed dispatches cannot create
arbitrary parallel groups. Existing Sol and sharded event behavior remains
unchanged.

Both normal and timeout scope artifacts record model, reasoning effort, service
tier, and verbosity. The model action pin, output schema, safety strategy,
read-only sandbox, exact three-dot diff, cancellation classifier, and finalizer
remain shared with the existing unsharded benchmark.

## Acceptance gates

1. The initial adjudicated run must complete all six cases. Any verified timeout
   or automation failure rejects the candidate before recall evaluation.
2. Human adjudication must recall both known `HIGH` findings without downgrade
   and at least 90% of the five known `MEDIUM` findings. With five findings, the
   threshold requires 5/5.
3. Every new `MEDIUM`, `HIGH`, or `CRITICAL` finding must be valid. Completed
   clean controls must not report an invalid material finding.
4. Repeat only disagreements, misses, or cases within 10% of the model budget.
   Repeats cannot erase an initial completion failure.
5. Run PRs #957 and #964 only if the adjudicated gates pass. Both must complete
   credibly before production evaluation.
6. Any production change requires a separate reviewed rollout with the current
   Sol configuration retained as rollback.

## Test plan

- Execute the trusted selector and assert the exact Terra model and Codex
  argument array.
- Assert Terra rejects non-`unified-40` context, non-`high` effort, and a
  non-baseline prompt.
- Assert the typed event routes to the unsharded bounded reviewer and finalizer,
  never the sharded workflow.
- Keep production and benchmark output schema, safety strategy, sandbox, exact
  diff generation, and timeout classification tests passing.
- Assert completed and timeout artifacts record service tier and verbosity.
- Run Ruff, the review-policy and sharding suites, Actionlint, and
  `git diff --check`.
- After this workflow lands on `main`, dispatch the initial adjudicated Terra
  run and adjudicate its artifacts manually.

## Verified upstream behavior

Verified on 2026-09-01 against OpenAI's live model documentation, the current
`openai/codex` configuration schema, and the pinned `openai/codex-action`
README:

- [`gpt-5.6-terra` model](https://developers.openai.com/api/docs/models/gpt-5.6-terra)
- [Latest model guide](https://developers.openai.com/api/docs/guides/latest-model)
- [Codex configuration schema](https://github.com/openai/codex/blob/main/codex-rs/core/config.schema.json)
- [Codex action arguments](https://github.com/openai/codex-action/blob/86365089eb2b84e0a8fb0717b304f8bdcb13b20e/README.md)
