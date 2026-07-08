---
title: "Live active-curtailment status"
date: 2026-07-03
status: implementing
type: plan
---

# Live active-curtailment status

## Summary

Active curtailment status should describe the event as it exists now, not only
the event-start decision snapshot. Operators should be able to glance at the
Energy active-curtailment card, header/status surfaces, and injected active
history rows and see the current targeted miner count and phase breakdown:
pending, dispatched, confirmed, drifted, unavailable, released, resolved, and
restore failed.

This is especially important after all-paired durable targeting. An event can
start with a small or stale `selected_count` snapshot and later own a much
larger set of targets as closed-loop admission, unavailable targets, or
all-paired policy rows change. The UI should present the live `target_rollup`
as the operational truth while keeping the decision snapshot as audit context.

No new schema or new user-facing workflow is expected. The existing active
curtailment UI should become more accurate using the `target_rollup` field that
already exists on `CurtailmentEvent`.

## Target UI/UX Shape

- The active curtailment card's "Applies to" count should use the current live
  target total when `target_rollup` is available.
- Active progress and restore calculations should continue to use phase counts,
  including `unavailable` targets introduced by all-paired curtailment.
- Multiple active events should each show live counts without requiring the user
  to select or hydrate every event detail row.
- Active events injected into the history list should use live target metrics
  when available, so filtered active/history views do not show stale selected
  counts.
- Completed/history rows should still be allowed to show event-start snapshot
  context where that is the intended audit view.
- `ListActiveCurtailments` should remain lightweight: no per-target rows and no
  full decision snapshot payload.
- If live rollup data is missing, the UI should keep today's fallback behavior
  rather than blanking active status.

The main acceptance case from the incident log is:

> An active event with `decision_snapshot.selected_count = 10` and
> `target_rollup.total = 5000` displays 5,000 as the live targeted count.

## Current Readiness

- PR #631 has landed, so all-paired curtailment target states and rollup fields
  are on `main`.
- `CurtailmentEvent` already has `target_rollup`, and
  `CurtailmentTargetRollup` already includes `unavailable`.
- Event detail, start, stop, force-release, and all-paired count-only responses
  already know how to populate rollups.
- The existing frontend mappers already convert `target_rollup` into display
  phase counts.

The remaining gap is active-list wiring. Today, `ListActiveCurtailments` returns
metadata-only events. It does not include `target_rollup`, and the active UI
still has paths that prefer `decision_snapshot.selected_count` over live totals.

## Implementation Approach

The implementation can be approached in a few equivalent ways. The important
contract is that active summaries carry live rollup counts without expanding
per-target payloads.

One low-risk backend shape is:

- Extend the active-event list query to aggregate target counts alongside the
  current event metadata.
- Keep the current "no decision snapshot" behavior for active polling.
- Map aggregate columns into `models.Event.TargetRollup` when converting active
  event rows.
- Populate `target_rollup` in `toListActiveCurtailmentsResponse`.
- Update comments in `proto/curtailment/v1/curtailment.proto` and server
  translator comments so the active-list shape is documented as metadata,
  scope, mode params, target-site coverage, and live target rollup.

An alternate backend shape is to have the service attach rollups after
`ListActiveEvents`. That is simpler mechanically but risks an N+1 query pattern
on a route that polls frequently. If that approach is chosen, it should batch
rollups or otherwise keep active polling cheap.

On the client side, the key behavior is to separate active/live display from
historical/audit display:

- Active-event mapping should prefer `targetRollup.total` over snapshot
  `selected_count` when calculating `selectedMiners`.
- Historical mapping can continue to use snapshot values first where that is
  the desired audit context.
- Active-history injection should use the same live-count behavior as the
  active card, because those rows represent active events.
- The active refresh fallback path should not let stale selected-event detail
  overwrite a fresher `targetRollup` from `ListActiveCurtailments`. If detail
  hydration fails, prefer the active-list row's rollup and state while retaining
  current detail-only fields only when the list row does not provide them.
- Existing fallback behavior should remain for old responses or partial data:
  if no `targetRollup` exists, derive counts from targets when present, then
  fall back to snapshot/empty values as today.

The UI can stay visually close to the current design. The goal is accuracy, not
new decoration. The active card should continue using the existing stats and
progress presentation; the displayed numbers should simply come from live
rollups when those are available.

## Test Plan

Backend tests:

- `ListActiveCurtailments` returns `target_rollup` for each active event.
- Active-list responses still omit per-target rows.
- Active-list responses still omit decision snapshots and scrub replay metadata.
- Rollups include all active target states, including `unavailable`.
- Events with no target rows return a zeroed rollup rather than failing.
- If the rollup is implemented in SQL, add or extend a DB-backed store test to
  verify aggregate counts across pending, dispatching/dispatched, confirmed,
  drifted, unavailable, resolved, released, and restore-failed rows.

Client tests:

- An active event with snapshot selected count `10` and live rollup total
  `5000` maps/displays as 5,000 targeted miners.
- A completed/history event can still use snapshot selected count as audit
  context.
- Active polling updates target counts from `ListActiveCurtailments`.
- If selected-event detail hydration fails, a fresh active-list rollup is kept
  instead of overwritten by stale current detail.
- Injected active rows in filtered history use live rollup totals and phase
  counts.
- Existing restore progress tests continue to pass with rollup-based totals.

Suggested targeted validation after implementation:

```sh
go test ./server/internal/handlers/curtailment ./server/internal/domain/stores/sqlstores
npm test -- activeCurtailmentData.test.ts useCurtailmentApi.test.ts ActiveCurtailmentStatus.test.tsx
```

## Notes And Boundaries

- This does not change target ownership semantics.
- This does not change all-paired selection or reconciliation behavior.
- This does not require returning target rows from active polling.
- This does not need to expose decision snapshots from `ListActiveCurtailments`.
- Structured command-preflight diagnostics remain a separate incident follow-up.
