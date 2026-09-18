---
title: "CT1 module and hashboard location breadcrumbs"
date: 2026-09-17
status: completed
type: plan
tracker: https://github.com/btc-mining/proto-fleet-internal/issues/113
---

# CT1 module and hashboard location breadcrumbs

## Outcome

Show physical location inline with the existing CT1 selectors:

| Surface | Location row |
| --- | --- |
| Module modal | `Tank 1 / [Module 2 selector]` |
| Hashboard / ASIC detail | `Tank 1 / Module 2 / [Board 3 selector]` |

Ancestors are non-interactive. The selector supplies the final segment without
repeating it in the breadcrumb. Both use `text-heading-200`; static labels and
slashes use `text-text-primary-50`, and selectors retain `text-text-primary`.
Long tank labels wrap while the selector remains visible.

The module modal no longer renders the Start run-in control. The standalone
run-in implementation remains available for future integration.

## Implementation

- Add a ProtoOS `LocationBreadcrumb` component that resolves the selected
  module's tank from the existing container topology. Board views resolve the
  selected serial to its board node, then follow its module and tank ownership.
- Match the Tanks page's labels: trimmed configured label, then `Tank {slot}`,
  then sorted display index when the slot is absent. Use `Module ?` when a
  module coordinate is missing. Suppress unresolved or mismatched ancestry.
- Reuse shared `Breadcrumb` with an optional styling override. Add an optional
  heading size to `InlineSelector`, retaining its default for other consumers.
- Render the module row in `ModuleModal` and the board row in
  `HashboardTemperature`. Existing selectors continue to handle selection and
  routing, including query parameters and embedded miner paths.
- Container-only ancestry leaves R2 without a breadcrumb and preserves its
  selector typography and spacing. No new routes, APIs, persistence, polling,
  generated code, or dependencies are required.

## Completed work

- [x] Add reusable ancestor rendering and topology fallback coverage.
- [x] Integrate the module and board rows with existing selectors.
- [x] Cover standalone and embedded direct links, delayed topology, selector
      changes, invalid membership, and Back/Done navigation.
- [x] Add long-label stories and inspect desktop and narrow layouts.
- [x] Apply the final typography and remove Start run-in from the module modal.

## Validation

Focused component and integration tests cover labels, missing topology, board
membership, selection changes, direct links, query parameters, and modal focus.
Client lint, TypeScript, and both application builds passed during implementation.

Local Chromium checks against Storybook verified the module-to-board flow,
selector switching, Back/Done navigation, long labels at 320–1440px, and matching
20px selector/breadcrumb sizes. An isolated R2 fixture showed no breadcrumb and
preserved selector placement. These checks used fixtures; physical CT1 hardware
and a live Fleet deployment were not exercised. No visual baselines were updated.

## Relevant existing guidance

Embedded navigation must retain the miner root prefix; see
[the navigation regression write-up](../../solutions/logic-errors/kpi-tab-navigation-needs-minerroot-prefix-2026-07-02.md).
The new static ancestors introduce no links, and existing navigation remains
responsible for preserving that prefix.
