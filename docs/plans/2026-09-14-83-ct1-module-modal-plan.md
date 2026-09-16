---
title: CT1 protoOS module modal (from tank view) and CT1-sized ASIC table
date: 2026-09-14
status: implementing
type: plan
tracker: https://github.com/btc-mining/proto-fleet-internal/issues/83
---

## Summary

GitHub #83 (part of epic #79). Clicking a module in the Tanks page opens a **module
modal** showing the module's three hashboards (per-board efficiency / hashrate / power /
ASIC avg / ASIC high + ASIC heat-map preview), the module's PSU under "Other hardware",
and module controls. Each hashboard card's **View** opens the existing full-screen ASIC
table (`HashboardTemperature`), which must render and scroll correctly at CT1's
12 × 26 = 312-ASIC size. There is no standalone module page.

**Design target:** the attached mock (fullscreen surface, header `✕ Modules … Done`,
`Module 1 ⌃⌄` switcher, "Hashboards" section with three cards, "Other hardware" section
with one wide PSU card).

**Done when:** on a CT1 device, `/tanks/:tankId/modules/:moduleId` renders the Tanks page
with the module modal open; the modal shows live per-board and PSU telemetry from the
normalized store; View opens the ASIC table for that board and Done/✕ returns to the
modal; the ASIC table renders 312 ASICs with serpentine labels against the CT1 sim;
`tsc`, `eslint`, targeted `vitest` pass; Storybook stories exist for the modal and the
CT1 ASIC table.

## Dependencies and base branch

PR #90 (issue #80) is merged into `main`. This branch is rebased onto `main`,
so the review diff contains only the module modal and related telemetry work.

- The merged Tanks page provides the tank grid, module entry point,
  `Modal.dialogLabel`, `TankNode.moduleLayout`, and hashboard health fields.
- `HashboardTemperatureWrapper` owns lazy ASIC fetching for the selected board;
  the modal polls only its three visible boards.
- Phases 1 and 2 are implemented. Module controls remain deferred pending design.
- Detailed CT1 component status is deferred to [#93](https://github.com/btc-mining/proto-fleet-internal/issues/93):
  canonical component selection, scoped error details, and restoring card info buttons.

## Findings that shape the design

1. **The Diagnostics CT1 branch is already 80 % of this modal.** `DiagnosticView.tsx`
   renders `HashboardsSection` (grid of `HashboardStatusCard`) and, for `moduleModel ===
   "ct1"`, `OtherHardwareSection` (PSU + controller cards). The mock is those two
   sections scoped to one module, inside a fullscreen surface. Reuse
   `HashboardStatusCard`, `PsuStatusCard`, `Card`, `CardHeader`, `LabeledValue`,
   `ComponentSection`, `AsicTablePreview`, `useHashboardLayout`.
2. **`HashboardStatusCard` is rig-shaped in four places** that must become
   device-adaptive or prop-driven: title `Slot {n}` (mock: `Board {n}`); four stats in a
   2-col grid (mock: five stats, adds Efficiency); `View` does `navigate(serial)` relative
   to the current route (works from `/diagnostics`, wrong from `/tanks/...`); error badge
   via `useErrorsByComponent("HASHBOARD", slot)` which reads root-module errors only and
   `MdkV2Adapter.getErrors()` returns `[]`, so on CT1 the badge never lights.
3. **CT1 PSU telemetry is not in the store yet.** `mdkV2/normalize.ts` sets
   `psuIds: []` and `psus: new Map()`. The wire data exists per hashboard
   (`V2HashboardHealth.psuElectrical`, `.psuThermal`, `V2Hashboard.psuIdentity`) and the
   PSU is shared per module. The CT1 sim does not emit those fields at all.
4. **`useMinerPsu(slot: number)` keys PSUs as `module:root:psu:{slot}`.** A per-module PSU
   needs a `ComponentId`-keyed selector; the numeric-slot API cannot address 288 modules'
   PSUs.
5. **Per-board ASIC max temp is missing for CT1.** Normalization sets
   `averageAsicTemperature` from `health.averageAsicTemperatureC` but never
   `maximumAsicTemperature`. The mock shows "ASIC high".
6. **ASIC positions come from the API, labels from client constants.**
   `toCanonicalHashboardStatus` maps `layout.entries[].x/y` → `column/row`; the
   serpentine labeller assumes 12 rows × 26 columns. **The CT1 sim emits 4 × 78**
   (`asicStackCount = 4`), so against the sim every column ≥ 26 gets an empty label and
   the grid is 4 rows of 78. Preserve API stack identities and ordering, and wrap
   each 78-ASIC stack into three display rows of 26 (user decision, 2026-09-14).
   Stack count and display row count are separate concepts; do not change simulator
   stack metadata to match the comp.
7. **ASIC telemetry for CT1 only arrives via `/hashboards/{id}/asics`.**
   `getCurrentTelemetry` for CT1 never returns ASIC-level data, so
   `useTelemetry({ level: ["asic"] })` is a no-op for ASICs on CT1. Live ASIC values need
   `useHashboardStatus(..., { poll: true })` scoped to the boards on screen.
8. **`HashboardTemperature`'s board switcher lists every board with a slot** —
   on CT1 that is up to 864 entries, all named `Hashboard 1..3`. It must be scoped to the
   current module.
9. **Module controls have adapter methods but no hooks and no UI.**
   `startScopedMining / stopScopedMining / setModuleFixedVoltage /
   clearModuleFixedVoltage / startModuleRunIn` exist on `MdkV2Adapter` (all optional on
   `MinerAdapter`). No GET for fixed-voltage state or run-in progress is wired in the
   adapter; the sim's run-in is a stateless stub. The mock shows no controls (design TBD).
10. **The scaffold route `tanks/:tankId/modules/:moduleId` currently renders a module KPI
    page** (`KpiLayout` + `kpiRoutes`, from #71). The epic decided there is no module page.

## Design decisions

### D1. The modal is URL-addressable: `/tanks/:tankId/modules/:moduleId`
Repurpose the scaffold route to render `<ContainerTanks />`; `Tanks.tsx` reads
`moduleId` from params (via existing `findModuleInTankByRouteParam`) and renders the modal
when it resolves. Close = `navigate` to `/tanks/:tankId`. Module switcher = `navigate(...,
{ replace: true })` to the sibling module. Benefits: deep-linkable (the epic's stated
goal for tanks), browser back works, and the ASIC table can return *into* the modal.
Drop the `kpiRoutes` children under that path (scaffold, superseded by the epic
decision). Leave `KpiLayout`'s module-selection code in place; deleting it is a separate
cleanup.

### D2. ASIC table route nests under the module: `/tanks/:tankId/modules/:moduleId/:serial`
Fullscreen route → `HashboardTemperatureWrapper`. `HashboardTemperature.close()` already
does `navigate("..", { relative: "path" })` and `HashboardSelector` does
`navigate("../" + serial)`, so with the serial as the last segment both land back on the
module modal / sibling board without touching that navigation code. Mirrors the existing
`diagnostics/:serial` convention. (A `/hashboards/:serial` sub-segment would break
`close()`.)

### D3. Fullscreen `Modal`, not a route page
`Modal size="fullscreen"` with `title="Modules"`, `buttons=[{ text: "Done", variant:
"primary" }]`, default ✕ icon, `dialogLabel`. This matches the mock's header and reuses the
overlay / escape / scroll handling. `HashboardTemperature` stays a fullscreen *route*
because it already is one and is shared with rigs.

### D4. Device variance goes through the `HashboardLayout` descriptor, not `moduleModel` ifs
Extend `HashboardLayout.card` with `title: (slot) => string` (`Slot n` / `Board n`) and
`showEfficiency: boolean`. Keep `showSlotIndicator` (turn it on for CT1 if
`HashboardIndicatorV2` renders a 3-position glyph acceptably; the mock shows one).

### D5. PSU becomes a first-class per-module component in the CT1 normalizer
One `PsuHardware` + `PsuTelemetry` per module, `ComponentId = "{moduleId}:psu:1"`, owner
`{ kind: "module", id }`, sourced from the module's boards' `psuElectrical` / `psuThermal`
(first board that reports them; they are the same physical PSU) and `psuIdentity`.
Populate `ModuleNode.psuIds`. Add a `ComponentId`-keyed selector; keep the numeric
`useMinerPsu(slot)` for rigs.

Normalize PSU thermal health by module, respecting `thermalStateValid`, so one
module's PSU warning cannot appear on another module. Select electrical and thermal
snapshots independently, preferring the freshest reported sample age. Missing
measurements display `—`; actual zero readings remain zero. PSU identity lives on
the board; electrical and thermal snapshots live under its health.

### D6. Controls land last and behind the design
Phase 3 adds a power switch (module scope, confirm dialog — same pattern as
`TankCard`), fixed-voltage set/clear, and run-in start, using new thin hooks over the
existing adapter methods with feature detection. No status readback exists for fixed
voltage or run-in progress; show the last command result in-session and file the API gap
against #88 (adapter method for `GET /api/v2/run-in/{taskId}` + a fixed-voltage GET).

## Work breakdown

### Phase 1 — Module modal (hashboards + PSU), built on merged PR #90

**Routing**
- `client/src/protoOS/router.tsx`: `tanks/:tankId/modules/:moduleId` → `<ContainerRoute><ContainerTanks/></ContainerRoute>`; remove its `kpiRoutes` children. Add
  `tanks/:tankId/modules/:moduleId/:serial` (fullscreen) → `HashboardTemperatureWrapper`.
  No new lazy factory needed (`importContainerTanks` and the temperature importer exist),
  but confirm `routePrefetch.ts` tiers still cover both. Update `router.test.tsx`.

**Tanks page wiring** (`features/tanks/`)
- `Tanks.tsx`: replace `selectedModule` state + inline `ModuleOverview` with route-driven
  open state (`moduleId` param → `findModuleInTankByRouteParam`). `onViewModule` navigates
  to `${minerRoot}/tanks/${tankRouteValue(tank)}/modules/${moduleRouteValue(module, i)}`.
  Handle "module not found" like the existing "Tank not found" status line.
- `TankModuleGrid.tsx`: leave the popover; its "View hashboard" links now point at the
  nested route from D2 (so ✕ on the ASIC table returns to the modal, not `/diagnostics`).

**New `ModuleModal`** (`features/tanks/components/ModuleModal/`)
- `ModuleModal.tsx`: fullscreen `Modal` (D3). Body: `ModuleSelector` heading,
  `ComponentSection "Hashboards"` → 3-col grid of `HashboardStatusCard` sorted by
  `location.slot` (fill missing slots with `EmptySlotCard`), `ComponentSection "Other
  hardware"` → `PsuStatusCard` per `module.psuIds` (wide variant) plus an empty state when
  none.
- Data: `useHashboardStatus({ hashboardSerialNumbers: <3 serials>, poll: true })` while
  mounted (finding 7; three requests per poll, fine). Tank page's existing
  `useTelemetry({ level: ["hashboard", "miner"] })` already feeds per-board stats and PSU
  once D5 lands; no extra telemetry hook.
- `ModuleSelector.tsx`: generalize `HashboardSelector` (`diagnostic/components/
  HashboardTemperature/HashboardSelector.tsx`) into a reusable inline picker
  (`items: {id, name}[]`, `currentId`, `onSelect`) and make `HashboardSelector` a thin
  wrapper. The mock's `Module 1 ⌃⌄` is exactly this control. Items = `tankModules(tank,
  topology)` from #90's `tankView.ts`; select → `navigate(..., { replace: true })`.
- Story: `ModuleModal.stories.tsx` seeded from #90's `Tanks.fixtures.ts` (healthy,
  attention board, missing board, no PSU data).

**`HashboardStatusCard` updates** (`features/diagnostic/components/HashboardStatusCard/`)
- Props: add `viewTo?: string` (absolute path; default keeps `navigate(serial)`), and
  `hasIssues?: boolean` override.
- Title via `layout.card.title(slot)` (D4). Stats grid: `grid-cols-2` → `grid-cols-2
  @md:grid-cols-3` inside the `@container` `Card`, add `EfficiencyValue` when
  `layout.card.showEfficiency`. Labels per mock: Efficiency / Total hashrate / Total power
  / ASIC avg / ASIC high.
- Issue badge: new store hook `useHashboardHasIssues(serial)` = root-module errors for
  that slot **or** `telemetry.hashboards.get(id).health ∈ {fault, degraded}` or
  `reportedIssueCount > 0` (fields from #90). Rigs keep current behaviour (health
  undefined).
- Verify the ⓘ `StatusModal` (`componentAddress: HASHBOARD/slot`) shows something
  sensible for CT1; if it is rig-only, hide it via a `layout.card.showInfoButton` flag
  rather than fixing StatusModal here.

**Store / normalizer**
- `store/contract.ts`: add PSU health to the existing telemetry type. If the
  `HashboardLayout` needs new fields they live in `diagnostic/hashboardLayout/types.ts`.
- `mdkV2/normalize.ts`: D5 PSU hardware + telemetry; set `maximumAsicTemperature` when the
  wire provides it (check `V2HashboardHealth` for a max-ASIC field; otherwise derive from
  the board's ASIC samples in `useHashboardStatus`'s merge, or leave `ASIC high` showing
  `—` and note it). Tests in `normalize.test.ts`.
- `store/hooks/useMiner.ts`: `useMinerPsuById(id: ComponentId): PsuData | null`.
  `PsuStatusCard` gains `componentId?: ComponentId` (mutually exclusive with `slot`);
  wide layout via `@container` query (`grid-cols-2 @3xl:grid-cols-6`) so the same card
  renders 2-col in Diagnostics and 6-col in the modal.
  Read PSU warnings by component ID and hide root-only status details for CT1.
  Preserve absent measurements instead of substituting zero; test partial data,
  zero readings, warning clearing, invalid thermal state, and module isolation.

**CT1 simulator** (`server/fake-ct1/`)
- Emit `psuElectrical` and `psuThermal` on each board's health, with `psuIdentity`
  and `psuType` on the board, consistent within a module. Extend `rest_api_handler_test.go`.

**Tests**
- `Tanks.test.tsx`: route with `moduleId` opens the modal; Done navigates to the tank
  route; switcher navigates between modules; module-not-found status.
- `ModuleModal.test.tsx`: renders 3 boards + PSU from fixtures; View link targets the
  nested serial route; polls only the module's serials.
- `HashboardStatusCard.test.tsx` (new or extended): CT1 title/efficiency/issue badge.
- `router.test.tsx`: new route resolves; old module KPI paths gone.

### Phase 2 — CT1-sized ASIC table

- Preserve simulator/API stack count, stack IDs, stack indices, and ASIC identities.
  Normalize API stack layout into 26-column display coordinates, wrapping each stack
  independently. Four stacks of 78 become twelve rows of 26. Preserve API-provided
  physical coordinates when they already describe the 26-column grid. Test all 312
  positions, stack boundaries, shuffled responses, and missing entries.
- `HashboardTemperature.tsx`: scope `hashboardList` to the current module on containers
  (`topology.hashboards.get(id).moduleId` → sibling boards), named `Board 1..3`. Rigs
  unchanged.
- `HashboardTemperatureWrapper.tsx` (#90 version): `poll: true` on mdk-v2 so the CT1
  ASIC grid is live (one board, cheap). Keep `useTelemetry({ level: ["asic"] })` as is; it
  is what refreshes per-board stats.
- Render/scroll check at 312: `CONTAINER_MODULE_LAYOUT` cells are `h-14 min-w-14 flex-1`
  → ≥ 1456 px wide, 12 rows ≈ 700 px tall, inside the existing `overflow-x-auto`
  container. Verify on laptop and tablet widths; check that 312 `AsicButton` +
  `PopoverProvider` pairs do not jank on poll (memoize `AsicButton` if the profiler shows
  re-render of all cells on each telemetry merge).
- `ContainerHashboard.stories.tsx`: add a 12 × 26 story; `AsicTable.stories.tsx` gets a
  CT1 variant.
- `layouts.test.ts` / `containerHashboardLayout.test.ts`: corner labels at 312
  (`F1`, `F26`, `F52`, `F27`, `A52`).

### Phase 3 — Module controls (design-dependent)

Deferred pending a controls design. Phases 1 and 2 implement the supplied comp;
this phase remains outstanding and the plan stays `implementing`.

- Hooks in `api/hooks/`: `useScopedMining` (start/stop with `MiningScope`; #90's
  `useTankPower` may already be this — reuse or generalize it), `useModuleFixedVoltage`
  (set/clear mV), `useModuleRunIn` (start with `ModuleRunInRequest`). Each feature-detects
  the optional adapter method and reports `supported`.
- `ModuleModal` header row: power `Switch` + confirm `Dialog` (copy `TankCard`), fixed
  voltage input + Set/Clear, Run-in button. Mining state from
  `rollupsByEntity.get(moduleId).miningStatus`.
- Sim: persist fixed voltage (already does) and reflect it in board health so the UI can
  read it back; optionally make run-in progress over time.
- File the API asks (fixed-voltage GET, run-in status via adapter) against #88.

## Risks and open questions

- **ASIC layout:** API identities and stack order take precedence; the user approved
  wrapping a stack across 26-column display rows. Display labels are not firmware IDs.
- **`StatusModal` on CT1.** The existing type-and-slot lookup cannot distinguish
  components across modules. Info buttons remain disabled until #93 adds canonical
  component selection and scoped CT1 error details.
- **Design TBD for controls.** Phase 3 layout is a guess; get the mock before building.
- **Deleting the module KPI routes** removes `/tanks/:t/modules/:m/hashrate|power|...`.
  Nothing links to them except `KpiLayout` breadcrumbs, but confirm with the epic owner.

## Verification

- `cd client && npx tsc -p tsconfig.json --noEmit && npx eslint <changed files>`
- `npx vitest run src/protoOS/features/containers src/protoOS/features/diagnostic
  src/protoOS/api/adapters/mdkV2 src/protoOS/router.test.tsx`
- `cd server/fake-ct1 && go test ./...`
- Manual: run fake-ct1 + protoOS dev server (see memory note for ports/proxy), open
  `/tanks/1`, click a module → modal; switch modules; View → 312-ASIC grid, ✕ → modal;
  Done → `/tanks/1`.
- Storybook: ModuleModal states, CT1 ASIC table.

## Implementation results — 2026-09-14

- Phases 1 and 2 implemented: route-driven module modal, module and board pickers,
  per-board stats and ASIC previews, module-owned PSU telemetry and thermal health,
  missing-value dashes, and a live 312-ASIC detail table.
- API stack metadata remains unchanged. The display wraps four stacks of 78 into
  twelve rows of 26; existing physical 26-column coordinates are retained. Tests
  cover partial stacks, missing positions, shuffled layout entries, and identity.
- ASIC maximum temperature comes from the selected board's finite ASIC samples.
  Root-only component info buttons are hidden on CT1; warning icons remain visible.
- `InlineSelector` shares the module/board picker behavior. The module modal manages
  keyboard focus, including the picker's portal and nested Escape dismissal.
- Validation: 189 client tests across 24 files passed, changed client files passed
  ESLint, and the ProtoOS production build (including TypeScript) passed. CT1
  simulator `go test ./...` passed.
- Chromium checks against the live simulator passed module switching, board
  switching, Done/close/back-to-module navigation, 312 cells in 12 rows, horizontal
  scrolling to the last ASIC, keyboard focus, nested Escape, and polling exactly
  three module boards. Modal widths checked at 1440, 768, and 375 pixels.
- All six new Storybook states rendered without runtime errors: healthy,
  attention, missing board, no PSU, partial PSU, and CT1 ASIC table.
- Screenshots and test logs are in `.context/`. No visual snapshot baselines were
  refreshed. Real CT1 hardware and module controls were not validated.
