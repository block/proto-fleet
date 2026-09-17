---
title: "CT1 protoOS module list"
date: 2026-09-16
status: completed
type: plan
tracker: https://github.com/btc-mining/proto-fleet-internal/issues/82
---

# CT1 protoOS module list

## Context

Keep the standalone Modules side-nav page as an all-tanks inventory. The user's
request and supplied comp resolve #82's question about whether this page should
exist. Rows open the existing module modal from #83; there is no module detail
page. Match ProtoFleet's MinerList appearance and interactions using the shared
List component, while retaining the existing CT1 page shell.

Inputs: the attached #82 issue export and Modules comp (2026-09-16). The comp's
right edge is cropped, so columns beyond the partially visible hashrate column
are proposed from the issue's requirements rather than inferred from the image.

### Findings from the current checkout

- The issue's `features/containers/pages/Modules/Modules.tsx` path is obsolete.
  The page is now `client/src/protoOS/features/modules/Modules.tsx`. Its all-tanks
  card grid currently has no click handler; its tank-scoped behavior survives in
  tests even though `/tanks/:tankId` now renders Tanks.
- `features/tanks/Tanks.tsx` already opens `components/ModuleModal/ModuleModal.tsx`
  at `/tanks/:tankId/modules/:moduleId`. The modal hardcodes tank navigation for
  dismissal, module switching, and board inspection.
- Shared `List` already supports filters (including an All filters dropdown),
  counts, controlled sorting, a sticky first column, horizontal scrolling,
  keyboard row activation, selection, and row actions. Reuse this surface.
- `MinerList.tsx` is the visual reference, not an import dependency. ProtoOS
  cannot import ProtoFleet code; Fleet's server pagination, authentication,
  bulk actions, and saved views are unnecessary for this bounded inventory.
- The normalized store already supplies module topology, module rollups,
  hashboard health, and reported issue counts. `tankView.ts` contains existing
  board/module status derivation. `MdkV2Adapter.getCurrentTelemetry` uses container
  map, mining status, and container health for hashboard-level polling.
- Module names and a single module MAC are absent: `ModuleNode` has identity,
  profile, tank, slot, and board IDs; wire `V2Module` has only ID and slots.
  MAC identities belong to hashboards.
- Board isolation currently becomes `health: offline`; it cannot be reliably
  distinguished in the normalized state. Run-in GET/list operations exist in
  the generated client but are not exposed by `MinerAdapter` or the store.
  These are data integration gaps, not presentation-only changes.
- Existing #83 plan notes about missing errors/PSU data are historical. Current
  normalization includes module PSU data and component-scoped errors.

## Approach

### Layout and display contract

Use the existing page heading and shared List styling: filter pills, count,
generous row spacing, subtle dividers, sortable headers, and sticky name column.
Keep table overflow usable on small screens; keep the filter/count controls
visible within the chosen scroll container. Reuse shared status and metric
formatters. Do not add a second table implementation or change the CT1 shell.

| Column | Source and proposed behavior |
| --- | --- |
| Name | Derive `T{X}-P{Y}` from the tank slot X and module position Y, e.g. `T1-P2` (user decision, 2026-09-16). Use actual topology positions, not list indexes; canonical module ID remains the row key. Show `?` for an unknown coordinate rather than inventing a position. |
| Position | Tank slot and module slot, e.g. `1-01`; unknown position is explicit. |
| Status | Reuse existing module status precedence; present healthy mining as `Hashing`, plus Needs attention, Sleeping, Offline, and Unknown. Add Isolated only when explicit isolation evidence is retained. |
| Issues | Aggregate current board issue counts; distinguish unavailable data from zero. Show counts or substantiated summaries, not an invented `Multiple failures` diagnosis. Scope any detailed issue UI by canonical module/component ID. |
| Hashrate | Module rollup with existing unit formatting; missing is `—`, actual zero remains zero. |
| Temperature | Module average temperature from the normalized rollup; honor °C/°F preference and show `—` when unavailable. |
| Power | Module board power from the normalized rollup, displayed in kW; preserve zero and missing readings. |
| Efficiency | Normalized module efficiency in J/TH; show `—` when unavailable. |

Filters: All filters, Status, Issues, Tanks. The nested and standalone
controls share filter keys. Tanks uses canonical tank IDs, and Issues initially supports With issues / No issues /
Unknown. Combine categories with AND and values within a category with OR.
Use client-side filtering and sorting over the loaded topology; begin with Name
ascending by numeric tank and position (so `T1-P2` precedes `T1-P10`) and a stable
ID tie-breaker; unknown coordinates sort last. Show `X of Y modules` when filtered and a
clear-filters empty state. No server pagination or virtualization unless measured
container-scale performance demonstrates a need.

### Navigation

Proposed route family: `/modules`, `/modules/:tankId/:moduleId`, and
`/modules/:tankId/:moduleId/:serial`. The latter two are modal/board overlays with
the Modules list as their background, not new detail pages. Keep the list shell
mounted across overlays so filters, sort, and scroll survive; persist filters
and sort in query parameters for reload/back behavior.

Give the existing ModuleModal explicit close, module-switch, and board-target
inputs so both Tanks and Modules can host it. Keep existing tank URLs working.
Use `topologyRouteParams.ts` and `minerRoot` for identity resolution and URL
construction; module slots are only unique within a tank. Preserve list query
parameters through module switching, board inspection, and dismissal. The board
view's existing relative parent close behavior fits the nested route shape, but
its selector navigation must also retain the query string.

Initially retain the modal's existing within-tank module switcher. Test direct
links, refresh, browser Back, invalid/missing entities, and standalone/embedded
hosting. Update `router.tsx` and `routePrefetch.ts` together for any added lazy
entry; reuse current import factories when possible.

### Data boundaries

Read topology and telemetry through ProtoOS hooks/store. Create a small pure
row projection under `features/modules/`; do not fetch inside cells. Use a
stable `useTelemetry` level array and existing polling/cancellation guards.
Only the open modal should request detailed ASIC readings.

Retain explicit board isolation in normalization before supporting an Isolated
status/filter. All isolated boards can produce Isolated; mixed isolation must
not label the whole module isolated. Board details remain available in the modal.
Keep status precedence consistent with Tanks by sharing the derivation and
testing any semantic changes for both pages.

Add an optional adapter read for run-in tasks using the existing generated list
operation, then a normalized resource/hook with freshness and source coverage.
Match tasks using canonicalized module/board identity, never a display slot
alone. Validate wire `moduleId` conversion and board membership with fixtures.
Handle multiple tasks explicitly: active tasks take precedence; conflicting
board outcomes display Mixed with details; otherwise show the latest terminal
result. Incomplete task coverage cannot imply Not run. Unsupported firmware
shows Unavailable without blocking the inventory. Verify deployed/simulator
responses before finalizing this mapping; generated types alone do not prove
endpoint availability. Do not hand-edit generated code.

### Scope assumptions and decisions still needed

- Draft assumes **list + modal only**. The comp's checkboxes and ellipsis menus
  are deferred until useful actions are specified; the user was asked whether
  actions belong in this pass. Do not ship inert selection controls. Module
  power, voltage, run-in commands, and bulk actions require their own action
  design and capability handling.
- Names are confirmed as position-derived `T{X}-P{Y}`. Omit MAC addresses because
  modules have no authoritative module-level MAC. Omit the board health column
  per user direction; retain module-level status and issues.
- Run-in status is part of #82's requested scope. The list can land incrementally,
  but #82 is not fully satisfied until status is integrated or explicitly
  deferred. Unsupported devices may honestly display Unavailable.

## Steps

Each step is a separately verifiable slice. Paths below are relative to
`client/src/` unless otherwise stated; new filenames are proposals.

### 1. Render the live module inventory (medium; no dependencies)

- [x] Replace the grid with `List<ModuleListItem, ...>` and a pure row projection.
- [x] Include every mapped module with stable IDs; render available columns and
  explicit missing-data states using real telemetry.
- [x] Cover initial loading, map failure, stale refresh, and empty topology.

Files: `protoOS/features/modules/{Modules.tsx,moduleListModel.ts,moduleColumns.tsx,
moduleListModel.test.ts,Modules.test.tsx}`.
Verify projection/component tests: position-derived names, duplicate slots across
tanks, missing coordinates, missing boards, null vs zero metrics, live updates,
and consistent status with Tanks.

### 2. Add filters and sorting (medium; depends on 1)

- [x] Implement the five filter controls, shared nested keys, accurate counts,
  stable sorting, and clear-filter behavior.
- [x] Preserve unrelated URL parameters; Back/Forward restores filter/sort state.
- [x] Polling does not reset the user's view or reorder tied rows arbitrarily.

Files: `features/modules/{Modules.tsx,moduleListFilters.ts,moduleListUrlState.ts,
moduleListFilters.test.ts,Modules.test.tsx}` under ProtoOS.
Verify combined filters, tank identity collisions, unknown URL values, sorting
numeric metrics/positions, and zero results. Checkpoint: usable live inventory.

### 3. Open the existing modal from the list (medium; depends on 1–2)

- [x] Add explicit navigation inputs to ModuleModal and adapt both hosts.
- [x] Register nested list modal/board routes and preserve the mounted list shell.
- [x] Keep existing Tanks entry, modal switching, and board inspection working.

Files: `protoOS/router.tsx`, `protoOS/routePrefetch.ts`,
`protoOS/features/modules/Modules.tsx`, `protoOS/features/tanks/Tanks.tsx`,
`protoOS/features/tanks/components/ModuleModal/ModuleModal.tsx`.
Verify typecheck and existing Tanks/modal tests; exercise list → modal → board →
modal → list manually. The next slice adds focused navigation regression tests.

### 4. Preserve navigation state and accessibility (medium; depends on 3)

- [x] Preserve query state through the board view and selector; restore list
  scroll and focus to the triggering row on dismissal.
- [x] Add regressions for reload, Back/Forward, escaped IDs, unknown tank/module,
  and a nonempty `minerRoot`.
- [x] Verify keyboard row activation, modal focus containment, and meaningful
  status labels without relying on color.

Files: `features/modules/Modules.test.tsx`, the hashboard temperature view/selector
and their focused tests (up to five files under ProtoOS).
Checkpoint: both list and tank modal entry flows work without navigation loss.

### 5. Retain isolation evidence (medium; depends on 1)

- [x] Preserve the wire isolation flag in the normalized board model.
- [x] Extend shared module status derivation for full/mixed isolation without
  guessing isolation from Offline.
- [x] Update the list status/filter presentation and regression tests.

Files: `protoOS/store/contract.ts`, `protoOS/api/adapters/mdkV2/normalize.ts`,
`protoOS/features/tanks/{tankView.ts,tankView.test.ts}`,
`protoOS/features/modules/moduleListModel.ts`. Verify existing adapter and Tanks
tests plus full/mixed isolation fixtures; check both page presentations.

### 6. Expose run-in task reads (medium; depends on identity mapping in 1)

- [x] Verify task response/coverage semantics with actual API or simulator data.
- [x] Add the optional read contract, normalized task result, and adapter mapping.
- [x] Test same-slot modules in different tanks, partial sources, mixed board
  results, missing identities, and unsupported endpoints.

Files: `protoOS/api/adapters/types.ts`, `protoOS/api/adapters/mdkV2/MdkV2Adapter.ts`,
and new `runIn.ts` / `runIn.test.ts` mapping files in that adapter directory.
Verify focused adapter tests; document any server gap before claiming support.

### 7. Show live run-in status (medium; depends on 1 and 6)

- [x] Add one page-level polling resource/hook, independent of core telemetry,
  using the existing cancellation/device-change protections.
- [x] Join normalized results to rows and distinguish no tasks, mixed status,
  unsupported, failed refresh, and incomplete coverage.
- [x] A task-read failure leaves the rest of the inventory usable; no request
  per row and no run-in mutations.

Files: new `protoOS/api/hooks/useModuleRunInStatus.ts` and its test,
`protoOS/features/modules/{Modules.tsx,moduleListModel.ts,moduleListModel.test.ts}`.
Verify lifecycle cleanup, identity matching, stale reads, and resource states.

### 8. Validate comp fidelity and regressions (medium; depends on 1–7)

- [x] Add module-list stories/fixtures covering mixed status, empty/error views,
  long identifiers, and container-scale data.
- [x] Check desktop/narrow layouts, both themes, sticky/overflow behavior,
  filters/count alignment, and modal return against the supplied comp/MinerList.
- [x] Run focused checks and test against the CT1 simulator; inspect console and
  network for errors, duplicate polling, and per-row requests.

Files: `protoOS/features/modules/{Modules.stories.tsx,Modules.fixtures.ts,
Modules.test.tsx}` plus targeted ProtoOS E2E coverage if its harness supports CT1.

## Verification and acceptance

Implementation commands verified from the current justfile/client package:

```sh
cd client
npx vitest run src/protoOS/features/modules src/protoOS/features/tanks src/protoOS/api/adapters/mdkV2
npx vitest run src/protoOS/features/diagnostic/components/HashboardTemperature
npm run build:protoOS
npm run lint
```

Also run focused tests for the new run-in hook. Run shared List tests and
ProtoFleet MinerList regressions if shared code changes. From the repository
root use `just dev-ct1` for real-browser verification: its default is 6 tanks ×
16 modules (96 rows); repeat with `CT1_MODULES_PER_TANK=48` to check 288 rows.
Use `just test-e2e-protoos` where the configured harness supports the scenario.
Do not refresh ProtoFleet expected screenshots without the explicit approval
required by AGENTS.md.

Complete when Modules looks and behaves like the shared MinerList pattern,
shows truthful live inventory/status data, filters and sorts correctly, opens
the existing modal, preserves list context through board inspection/dismissal,
and leaves the Tanks flow working. Implementation and verification results are recorded below.


## Implementation record (2026-09-16)

This records the initial implementation. The metric-column follow-up below
supersedes run-in support, which was removed at the user’s request.

Implemented the shared List inventory, position-derived names, URL filters and
sorting, list-hosted module/board overlays, explicit isolation evidence, and
optional run-in reads. Added stories and regression tests. Selection and write
actions remain outside this accepted list-and-modal scope.

Implementation details that refine the plan:

- Modules keeps the list mounted through nested overlay routes. The existing
  ModuleModal accepts route/label inputs; tank URLs retain their behavior.
- MAC address and board health columns are omitted per user direction. Module
  status and issues still derive from board telemetry. Missing coordinates use `?`.
- The shared filter component places All filters after the individual filters;
  the page retains that existing interaction/layout convention.
- Run-in uses a page-level resource hook rather than adding another store slice.
  Device identity, cancellation, and the existing write guard prevent stale
  responses. The inventory suspends its telemetry polling during board inspection.
- Independent board run-in results use each board's latest task; mixed results
  stay visible. Missing source/board coverage is labeled partial. Unsupported
  firmware displays Unavailable, and failed reads display Unknown.

Verification:

- ProtoOS production build and client lint passed. Build output retains existing
  Vite configuration, CSS optimization, and chunk-size warnings.
- All 818 ProtoOS tests passed across 105 files, covering projections, filters/history, modal and board navigation,
  invalid links, route guards, isolation, run-in normalization and cancellation.
- Headless Chrome exercised the actual CT1 simulator with 96 and 288 modules:
  filtering/reload, numeric sorting, modal → board → modal → list, focus return,
  direct links, and dark mode. No browser exceptions were observed.
- Responsive checks at 320, 768, 1024, and 1600 pixels confirmed the table scrolls
  within the page without expanding document width.
- The CT1 simulator does not implement `GET /api/v2/run-in`; verified its
  unsupported behavior directly. Supported task rendering was verified with
  generated-contract fixtures and browser response interception, not live
  run-in-enabled firmware. This remains the validation limit for run-in data.
- Browser scripts, screenshots, and check output live in gitignored `.context/`.
  No expected screenshot baselines were refreshed.


### Scroll behavior follow-up (2026-09-16)

Modules uses a full-width content scrollport below the ProtoOS header, with
24px gutters on phone/tablet and 40px on laptop/desktop. Gutters belong to table
cells so hover backgrounds span the entire available width. Shared List's new
`overflowContainer="page"` option delegates both scroll axes to that scrollport
at every breakpoint; existing boolean options retain their behavior. Column
headers pin below the fixed app header. `scrollLeadingPadding` lets the name
column's gutter scroll away before its content pins on tablet and larger screens;
phones retain MinerList's unpinned columns so all values remain reachable.
Heading, filters, and count use the scrollport's container width to stay aligned
during horizontal scrolling, including when a vertical scrollbar occupies space.

Verification: 124 targeted Modules, router, and shared List tests passed, along
with TypeScript, targeted lint, and the ProtoOS production build. Chrome checks at 320, 768, 1024, and 1600px
verified full-width rows, both scroll endpoints, sticky header position, and the
name column's gutter threshold. Module → hashboard → module → list navigation
still preserves filters, sorting, and focus. Screenshots and measured geometry
are saved in `.context/modules-scroll-*`; no snapshot baselines were replaced.

Type filtering was removed per user direction: CT1 is the only supported module
type. Status, Issues, and Tanks remain available individually and in All filters.


### Metric-column follow-up (2026-09-16)

Replaced Run-in with sortable Temperature, Power, and Efficiency columns. The
row projection reads existing per-module rollups: average board temperature in
°C, board power in W, and efficiency in J/TH. Rendering respects the temperature
preference and converts W to kW. Missing/non-finite readings display `—`, real
zeros remain zero, and unavailable metrics sort last in both directions.
Removed the unused run-in hook, mapper, adapter method, and their tests; no
run-in requests remain on this page. The original plan's run-in work is now
outside the accepted scope.

Validation: 35 module/telemetry tests passed, including unit preferences, metric
sorting, zeros, and absent readings. Chrome rendered live simulator values and
confirmed metric sorting and no run-in requests. Existing scroll behavior was
rechecked with the wider table at four viewport sizes.


### Gutter refinement (2026-09-16)

Aligned the Name header and row text by removing duplicate inner left padding
from the first data column when the scrollable gutter is enabled. Modules now
uses Home's responsive gutters: 24px on phones, 40px on tablets, and 56px on
laptop/desktop. Heading, filters, count, and table text share that inset. The
sticky name header and cells retain an 8px inset after the gutter scrolls away;
the shadow threshold follows the same inset. This supersedes the earlier
24/40px gutter sizing.

### Module actions follow-up (2026-09-17)

The user expanded the accepted scope to include selection and module commands,
superseding the initial action deferral. Added row checkboxes, select/deselect
all for the filtered list, component-error indicators, row action menus, and a
bulk action bar. Promoted Fleet's generic ActionBar and RowActionsMenu to shared
components and updated Fleet consumers; feature-specific Fleet actions remain
in Fleet.

Both menus offer Start mining, Stop mining, Blink LEDs, Isolate, and Remove
isolation. Blink LEDs matches the ProtoFleet action label; Stop identifying was
removed at the user’s request. Manual power controls require prior isolation and are
excluded. Row commands target only that module's hashboards, independently of
bulk selection. Bulk commands target the selected modules' hashboards. The
confirmation shows the captured module names and board count. Requests always
use explicit, deduplicated board identities, reject incomplete or changed
topology, and retain device-change cancellation and authentication retry guards.
Results report accepted counts and per-board failures; a telemetry refresh
failure cannot replay an accepted command.

Validation: 135 test files passed (1,187 tests passed, one skipped), covering
ProtoOS, shared components, and affected Fleet consumers. Client lint and both
ProtoOS and ProtoFleet production builds passed. Chrome checks verified
select-all/mixed selection, filtered bulk targeting, row targeting independent
of selection, partial failures, mobile confirmation/cancellation, and gutter
and sticky geometry at four viewport sizes. Browser command responses were
intercepted; live firmware command execution was not exercised. Screenshots and
scripts remain in `.context/`; no expected screenshot baselines were replaced.
