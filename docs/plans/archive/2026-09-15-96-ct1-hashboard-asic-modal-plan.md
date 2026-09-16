---
title: CT1 protoOS hashboard ASIC view at 312-ASIC density
date: 2026-09-15
status: completed
type: plan
tracker: https://github.com/btc-mining/proto-fleet-internal/issues/96
---

## Summary

GitHub #96 (part of epic #79, split from #83). Make the hashboard ASIC view reached from
the module modal's per-board **View** button work at CT1 density: 12 display rows × 26
columns = 312 ASICs, 40 px label-only cells, serpentine labels, top/bottom rails,
live-telemetry popovers, metric-aware heatmaps, and a five-stat header that matches the comp.

**Design target:** the attached comp (fullscreen surface, header `← Hashboards … Done`,
`Board 1 ⌃⌄` switcher, five stats, metric segmented control, `Top` rail with serial,
26-wide serpentine grid grouped in letter pairs). Figma only has the module modal frame
(`549:137529`, "Module FS modal"); there is no Figma frame for the hashboard view itself.
The comp image on the ticket is the destination design.

**Done when:** on a CT1 device, `View` on a board card opens the board's 312-ASIC view;
`←`/Done return to the module modal; the grid is legible and scrollable with rails pinned;
clicking a cell opens a correctly placed popover at every grid edge and while scrolled;
board switching stays in the view; switching metrics changes the heatmap as well as the
popover's selected reading; R2 sizing and default temperature appearance are unchanged
(restored popovers and corrected metric colors are intentional shared behavior changes);
mount/scroll cost is recorded from the CT1 sim; stories and tests cover CT1 at 312.

## What the current build does (inspected 2026-09-15, CT1 sim, 1440×900)

Baseline from `main` at `57fb756c6` running against `fake-ct1` (1 tank, 4 modules):

| Item | Observed |
|---|---|
| Entry point | `HashboardStatusCard` **View** → `navigate(viewTo)` → route `tanks/:tankId/modules/:moduleId/:serial` (fullscreen `HashboardTemperature`). Works. |
| Return | Header ✕ / Done → `navigate("..")` → Tanks route with module modal open. Works. Module modal remounts and refetches its 3 boards. |
| Board switching | `HashboardSelector` → `navigate("../<serial>", { replace: true })`. Stays in the fullscreen view. Works. |
| Grid | 312 cells render, 56×56 px each. Grid scroll width 1612 px in a 1440 px viewport, so it scrolls horizontally even on a large laptop. Cells cut off after column 23. |
| Rails | Top and bottom rails sit outside the scroller, so they stay pinned while the grid scrolls. Good. Both rails read `Top` / `Bottom` with **no temperature** against the sim. |
| Letter-pair grouping | Uniform 4 px row gap. Comp shows a larger gap between letter pairs (F / E / D …). |
| Stats | Four stats: Highest ASIC temp, Avg ASIC temp, Board power usage, Board hashrate. Comp: five (High ASIC temp, Avg ASIC temp, Power usage, Efficiency, Total board hashrate). |
| Metric tabs | Temperature, Hashrate, Voltage (mV), Frequency. Comp order: Temperature, Hashrate, Frequency, Voltage. Only printed values follow the selection: `AsicTable` always passes `asic.temperature.latest.value` to `getAsicColor`. |
| Header icon | ✕ (Dismiss). Comp: ← back arrow. |
| ASIC popover | **Never opens.** `AsicButton` has no click handler; the upstream import (`2712d0da0`) already had it commented out pending more ASIC data. Comment cleanup (`4958511d3`) later deleted it. `showPopover` state and 312 `PopoverProvider`s exist but are dead weight. |
| Popover render mode | Default `inline` (absolute inside the cell). The grid scroller is `overflow-x-auto`, which forces `overflow-y: auto`, so an inline popover above row F or beside column 26 would be clipped. |
| Perf | Client-side mount of the 312-cell grid: **86 ms**, two long tasks (76 ms, 60 ms). 12 s of polling: **no long tasks**. JS heap 82 MB. |
| Preview heatmaps | Module modal cards render 12 × 26 at 16 px rows; visually match the Figma card. The `attention` fixture cluster renders red (≥82 °C) where the Figma shows orange; both are correct per thresholds. |
| Labels | `getContainerAsicLabel` tests already cover F1, F26, F52, F27 and out-of-range → `""` at 26 columns. |

## Decisions

### D1. Keep the fullscreen route; do not stack a modal on the module modal
Already decided in #83 (D2/D3) and matches the comp, which is a fullscreen surface with a
back arrow, not a dialog over the module modal. A stacked modal would keep the module
modal's three preview polls alive underneath the board view (four ASIC fetches per poll
for one visible board), which #96's data note forbids. The route keeps exactly one board
polling and stays deep-linkable. Cost: the module modal remounts on return and refetches
three boards once. Acceptable; previews render from the store immediately.

### D2. Restore click-to-open popovers with live telemetry and one delegated provider

**Investigation (2026-09-15):** the imported `AsicButton.tsx` at `2712d0da0` says
`TODO: removed temporarily until asics have more data to show in the popover`.
`4958511d3` only removed that commented-out handler. The original disabling commit
predates this repository's source import; no more specific historical API or performance
rationale was found in the inspected local history or `docs/solutions/`.

There is a concrete limitation today: `api/adapters/mdkV2/MdkV2Adapter.ts:getTimeSeries`
supports miner and explicitly scoped hashboard histories, and rejects ASIC history.
`MdkV2Adapter.test.ts` asserts that ASIC history throws rather than returning an aggregate.
MDK v1 has ASIC history support, so this is not a claim that every device API lacks it.
The CT1 board poll already writes temperature, hashrate, voltage, and frequency into
each ASIC's `latest` readings (`api/hooks/useHashboardStatus.ts`). It does not populate
ASIC history. The existing `AsicPopover` therefore spins indefinitely without history,
and incorrectly derives "Current hashrate" from a historical chart point.

**Decision:** restore popovers using the selected ASIC's latest telemetry only. Display
temperature, hashrate, voltage, and frequency, emphasize the selected metric, and update
from the existing board poll. Missing readings show `—`; valid zero stays zero. Remove
the chart/spinner from the active popover path and read current hashrate from
`asic.hashrate.latest`. No ASIC history request, synthetic history, or extra polling.
Historical charts can be a separate follow-up once API support and cost are established.

The acceptance criteria require popovers to open, position, and dismiss. Wire
`onClick` on `AsicButton` (toggle `showPopover` by ASIC id). Replace the 312 per-cell
`PopoverProvider`s with **one** provider at `AsicTable`, setting `triggerRef.current` to
the clicked cell (the `TankModuleGrid` pattern). Set render mode `portal-scrolling` so the
popover portals to `body` and `usePopoverPosition` tracks the horizontal scroller; the
existing viewport-overflow flip handles the top row and last column. Store the open ASIC
identity and resolve its current data on each update; clear selection on board changes.

### D3. Device variance stays in the `HashboardLayout` descriptor
Every CT1-specific change below is a descriptor field, not a `moduleModel` conditional:
cell width, whether to print the metric value, letter-pair gap, header icon, and
detail-screen stat list. Metric tab order and metric-color semantics stay shared.
`RIG_LAYOUT` keeps its current sizing and printed values. R2's default temperature
appearance remains unchanged; D2 and D6 deliberately update shared interactions.

### D4. CT1 uses 40 px label-only rectangles and explicit stack spacing

Use fixed **40 px width** (`w-10 min-w-10 shrink-0`, no `flex-1`) and retain `h-14`.
Print the ASIC label only; the fill represents the selected metric, whose numeric value
is available in the popover. Add `layout.cell.showMetricValue` (false for CT1, true for
rigs). 26 × 40 + 25 × 4 = **1140 px** before surrounding page space. Scroll when the
available content width is smaller; keep rails pinned. Do not widen cells to fill a
large viewport or fall back to the superseded 44/48 px floor proposal.

Use **4 px within each two-row letter stack and 24 px between stacks**, confirmed in
review after measuring the comp. This is a 24 px total edge-to-edge gap, not 24 px plus
the existing 4 px gap.

**Comp check:** the attached 2560 × 1800 PNG (`.context/attachments/VgROSv/image.png`,
also attached to #96) has approximately 80 px wide cells, 8 px between F sub-rows, and
48 px between F and E. At an inferred 2× export scale, that agrees with 40 px cells and
4 px within-stack gaps, but suggests **24 px between stacks**, not 16 px. Example image
coordinates: F row edges y=711–838 and 847–974; E starts y=1023. The export scale is
inferred, not Figma metadata. The user confirmed 24 px after this measurement, replacing
the initial 16 px request.

### D5. Skip virtualization
Measured mount and poll costs are well under the threshold where virtualizing 12 rows
would pay for its complexity. Do the cheap wins (single provider, `React.memo` on
`AsicButton` with an `isOpen` boolean instead of the whole `showPopover` string) and
re-measure.

### D6. Heatmap colors follow the metric and its meaning

Read `selectedMetric` once in `AsicTable`, select that metric's latest measurement,
and compute the board's color scale once per update. Reuse existing theme tokens;
do not apply Celsius thresholds to another metric or silently fall back to temperature.

| Metric | Color meaning and scale |
|---|---|
| Temperature | Preserve current absolute Celsius thresholds and cool/warning/critical colors: hotter becomes adverse. Unit preference changes labels, not thresholds. |
| Hashrate | Higher is favorable: use a sequential success/green scale from zero to the visible board's maximum, with low output pale and higher output stronger green. Label the scale as board-relative output, not health or target attainment; do not invent failure thresholds without an expected rate. All-zero output gets an explicit zero reading and the low endpoint, not an all-healthy fill. |
| Frequency | Neutral informational sequential scale over the visible board's finite min/max. Higher frequency alone does not establish health or efficiency. |
| Voltage | Neutral informational sequential scale over the visible board's finite min/max; neither high nor low is inherently good without an operating target/range. |

Show a compact legend with metric, units, endpoints, and direction so relative colors
are interpretable; update it together with the cells. Equal finite values use one
consistent fill without division by zero. Missing/non-finite readings use a distinct
unknown fill and `—`, never a favorable color. Canonical units drive comparisons;
display temperature in the user's unit, hashrate in GH/s, frequency in MHz, voltage in mV.
Validate contrast in both themes and include the selected reading in each button's
accessible name so color is not the sole way to obtain its value.

## Work breakdown

### 1. Header, stats, tabs (comp fidelity)
- `HashboardTemperature.tsx`: header icon from `layout.headerIcon` (`←` "Back to module"
  for CT1, ✕ for rigs). Title stays "Hashboards".
- Stats from `layout.detailStats` builder so CT1 shows five stats in comp order (High ASIC
  temp, Avg ASIC temp, Power usage, Efficiency, Total board hashrate) and rigs keep their
  four. Efficiency via `efficiencyValue(power, hashrate, hashboard.efficiency)`. `Stats`
  gets `grid="grid-cols-5 phone:grid-cols-2"` for CT1 (the wrapper already scrolls on phone).
- Segmented control order → Temperature, Hashrate, Frequency, Voltage (matches
  `AsicTable.stories`). Keep `(mV)`; the code deliberately shows millivolts.

### 2. Grid fit at 312
- `layouts.ts` `CONTAINER_MODULE_LAYOUT.cell.frame`: `h-14 w-10 min-w-10 shrink-0 p-0`
  (D4); add `cell.showMetricValue: false` for CT1, true for rigs, and honor it in
  `AsicButton` so only the label is printed in CT1 cells.
- New `layout.grid.rowClass(row)` → `mt-5` on even rows after the first for CT1;
  with the existing `gap-1`, this gives 4 px inside a stack and **24 px total** between
  stacks. `""` for rigs. Apply in `AsicTable`.
- Confirm rails remain outside the scroller (they are) and add a right-edge fade or
  `scroll-shadow` only if the design asks; not in scope by default.

### 3. Popovers (D2)
- `AsicButton.tsx`: `onClick` toggles `setShowPopover(isOpen ? undefined : currentAsicId)`
  and sets `triggerRef.current`. Prop `isOpen: boolean` replaces `showPopover: string`.
  Wrap in `React.memo`.
- `AsicTable.tsx`: one `PopoverProvider` around the grid; `setPopoverRenderMode("portal-scrolling")`
  via a small inner component; render `AsicPopover` once for the open cell instead of inside
  each button.
- `AsicPopover.tsx`: remove the active chart and history-dependent loading state; all four
  rows read `.latest`, with units and missing/zero handling per D2. Emphasize the selected
  metric. Verify `closeIgnoreSelectors={[".asic-button"]}` still lets clicking another
  cell move the popover rather than just closing it. Keep the selected ASIC live as the
  store updates and dismiss when changing boards.
- Add coverage for keyboard activation, Escape/outside-click dismissal, live updates,
  latest-only samples with no history/spinner, correct current hashrate even when an old
  history point disagrees, missing values, zero, selected metric, and board changes.
  Opening/switching cells must not start requests or change the board polling cadence.

### 4. Labels
- Add `layouts.test.ts`: `CONTAINER_MODULE_LAYOUT.labelCell` / `labelDetail` return `""`
  when `row` or `column` is undefined; A-row corners (row 10 col 0 → `A1`, row 11 col 0 →
  `A52`, row 11 col 25 → `A27`). Existing `containerHashboardLayout.test.ts` already covers F.

### 5. Board switching
- No code change. Add a test in `HashboardTemperatureWrapper.test.tsx` or a new
  `HashboardSelector.test.tsx` that selecting Board 2 replaces the URL serial and stays on
  the nested module route.

### 6. Metric-aware heatmaps and board previews
- `AsicTable.tsx`: replace the hard-coded temperature color input with the selected
  metric and implement D6's color policy and legend. Resolve palette and scale once per
  grid, preserving the existing optimization that avoids per-cell `getComputedStyle`.
- Keep the temperature helper's existing callers stable; add a focused metric color
  resolver beside `kpis/hooks/asicColor.ts` rather than making shared previews depend on
  protoOS context. Cover reversed temperature/hashrate direction, neutral voltage and
  frequency, unit normalization, missing/non-finite values, equal values, and all-zero
  output. Component coverage must change the selector and assert fills, legend, and
  selected popover reading change together.
- `HashboardStatusCard` previews have no metric selector and explicitly supply ASIC
  temperature to shared `AsicTablePreview`. Keep these as temperature summaries at CT1
  aspect; add an assertion in `HashboardStatusCard.test.tsx` that the `attention` fixture
  renders actual warning/critical colors as well as the expected `data-value` readings.

### 7. Rail temperatures (found during inspection; confirm scope with the ticket owner)
- Rails show `Top` / `Bottom` with no value against the sim. `normalize.ts:409` sources
  inlet/outlet from `plant`; check whether CT1 boards ever carry these or whether the
  module/tank reading should be used. If it is a sim gap, file it against #88 rather than
  fixing here.

### 8. Stories and perf record
- `ContainerHashboard.stories.tsx`: add a `HotCluster` variant matching the comp (E10–E14,
  E38–E44, D9–D15, D39–D43 in the warning band, E40–E42 / D11–D13 critical).
- `AsicTable.stories.tsx`: add a `Ct1` story seeded with 12 × 26 positions.
- Include all four metrics, an open live-only popover, missing/zero samples, and both
  themes; assert CT1 cells are 40 px wide, omit numeric values, and have the agreed
  intra-/inter-stack gaps. R2 keeps its sizes and printed metric values.
- Re-run the mount/poll measurement script (`/tmp/perf-mount.js` pattern: `history.back()`
  then `forward()`, count `.asic-button` to 312, PerformanceObserver on `longtask`) after
  changes and record before/after in the PR.

## Verification

- `cd client && ./node_modules/.bin/tsc --noEmit && ./node_modules/.bin/eslint <changed> --max-warnings 0`
- `CI=true ./node_modules/.bin/vitest run src/protoOS/features/diagnostic src/protoOS/store/utils`
- Sim: `cd server/fake-ct1 && HTTP_PORT=18080 GOWORK=off CT1_TANKS=1 CT1_MODULES_PER_TANK=4 go run .`
  Client: `cd client && PROXY_URL=http://localhost:18080 ./node_modules/.bin/vite --mode protoOS`
- Manual at 1280 / 1440 / 1920 / 2560: open `/tanks/1/modules/1`, View → grid fits or scrolls
  with rails pinned; click F1, F26, F52, A52, A27 and a mid cell while scrolled → popover
  visible and inside the viewport; Escape and outside-click dismiss; switch Board 1→2→3; ←
  returns to the module modal.
- Verify computed width is 40 px, labels remain readable with no printed metric values,
  vertical gaps are 4 px within / 24 px between stacks,
  and keyboard users can open each cell and read its selected metric.
- Switch all four metrics and inspect fills/legend in both themes. In network tools,
  confirm an open popover updates on the existing board poll with no history requests
  or new per-ASIC polling; no permanent chart spinner when only current readings exist.
- R2: default temperature appearance and cell dimensions/values unchanged (visual compare
  `AsicTable` default story); restored live popovers and selected-metric colors work.

## Risks and open questions

- **Original history is incomplete.** The upstream import preserves a lack-of-data
  rationale, but not the original disabling change. No evidence found establishes a
  chart performance regression. CT1's current lack of ASIC history is independently
  verified in the adapter and its test; live-only popovers avoid that dependency.
- **Relative colors are not alarms.** Hashrate/voltage/frequency have no established
  per-ASIC operating thresholds in this plan; legends must disclose board-relative
  scales and never label an ASIC healthy based on magnitude alone.
- **Voltage unit**: comp says `(V)`, code shows `(mV)` deliberately. Keep mV unless design
  objects.
- **Rail temperatures empty** against the sim (item 7). May be a sim or normalizer gap
  outside this ticket.

## Review decisions and changelog

- 2026-09-15: Updated D2 after tracing the disabled handler, current adapter, and store
  writes. Restore live-only popovers; defer history. This supersedes the original
  assumption that `AsicPopover.tsx` would require no changes.
- 2026-09-15: Replaced D4's proposed 44/48 px growing cells with the requested fixed
  40 px label-only cells. Recorded measured comp spacing and the explicit 4/16 px request.
- 2026-09-15: Expanded item 6 with metric-aware colors, explicit color semantics and
  legends, edge-case tests, and shared R2 behavior expectations. Board-card previews
  remain temperature summaries because they have no selector.
- 2026-09-15: User confirmed the comp's 24 px inter-stack gap, superseding the initial
  16 px request. Updated D4, implementation classes, and verification to 4/24 px.
- Verification of this plan update: inspected repository history, current API/store/UI
  source, and PNG pixel coordinates; checked the Markdown diff. No application code
  changed and no runtime or performance measurements were rerun for this review.


## Implementation results (2026-09-15)

Implemented the approved grid, popover, metric, and header work:

- CT1 cells measure 40 × 56 px, show labels only, and have 4 px within-stack and
  24 px inter-stack gaps. R2 retains its sizing and printed values; its default
  temperature view does not gain a legend. Other metric views explain their scale.
- One provider and one keyed popover serve the grid. Current readings come from
  `.latest`, including real zeros; missing/non-finite readings show a dash. ASIC history
  is not requested. Board changes clear the selected ASIC.
- The selector controls colors, accessible readings, legend, and highlighted popover
  row. Temperature keeps the existing absolute thresholds; hashrate uses a favorable
  green output scale, and voltage/frequency use neutral ranges. An entirely missing
  metric does not invent a zero-valued range.
- CT1 has five ordered stats and calculated/reported efficiency, the back arrow, and
  the corrected tab order. Explicitly anchoring the fixed header with `top-0` also
  prevents it from covering the board selector after route entry.
- Device differences remain in the layout descriptor. The stat descriptor is an
  ordered metric/label list rather than a rendering callback; the screen resolves the
  current values. Metric helpers are colocated in `Asic/metricDisplay.ts` because their
  units/labels are shared by the grid and popover; the shared temperature helper is
  unchanged.

### Validation

- Targeted diagnostic and store utility suite: **86 tests across 15 files passed**.
- Changed-file ESLint, TypeScript `--noEmit`, and the ProtoOS production build passed.
- Chromium against the existing fake CT1 service: 1280/1440/1920/2560 px; 312 cells;
  F1/F26/F52/A52/A27/D13 popovers inside viewport bounds; keyboard/outside dismissal;
  all metrics in light/dark themes; Board 1 → Board 2 stays in the module route;
  Back/Done returns to the module. No browser errors observed.
- At 1000 px, the ASIC grid scrolls horizontally while rail labels remain stationary;
  an unclamped open popover follows its trigger as the grid scrolls.
- A live popover updates during a 12-second polling window with only the visible
  board's ASIC endpoint requested, plus existing application-level polling. No ASIC
  history requests or per-cell polling were introduced.
- CT1 and R2 table stories rendered (312/100 cells), along with all four CT1 metric
  variants and the full-screen hot-cluster fixture.
- Warm route-entry mount: **24.6 ms**, zero long tasks. The earlier plan recorded 86 ms;
  these are individual local development-browser observations, not a controlled
  hardware benchmark. The 12-second polling check also had zero long tasks. Browser
  automation click overhead is excluded from the 24.6 ms by starting at the captured
  View click and stopping when the 312-cell DOM appears.
- Scripts, JSON measurements, and screenshots are saved under `.context/96-*` for local
  review. No checked-in visual snapshot baselines were refreshed.

### Follow-up outside this change

Rail temperature values remain absent on CT1. Inspection confirms the current v2
normalizer puts inlet/outlet readings on plant telemetry and does not populate the
hashboard fields consumed by these rails. Reusing a tank/plant reading as a board
reading needs an explicit physical mapping decision, so no synthetic fallback was
added. Item 7 remains a separate data-contract follow-up; the approved grid and
popover scope is complete.


## Responsive cell follow-up (2026-09-15)

The user superseded D4's fixed-width, label-only choice: CT1 cells now have a **40 px
minimum** and grow equally to fill each row. At **56 px cell width and above**, the
selected metric value appears beneath the label. Below that threshold the label and
heatmap remain visible, with the value available in the popover and accessible name.
The cell height and 4/24 px vertical gaps are unchanged.

`layout.cell.metricValueVisibility` replaces `showMetricValue`: rigs use `always`, CT1
uses `when-wide`. A named CSS container query on each CT1 cell handles the threshold;
no JavaScript resize observers or viewport-width assumptions are needed.

Verification: diagnostic tests (62) and TypeScript/changed-file lint passed. Chromium
confirmed rows fill their container at 1280/1440/1920/2560 px, preserve the 40 px minimum
and scrolling at 1000 px, hide values at 55.95 px, and show them at exactly 56 px. An open
popover remains visible while resizing across the threshold. Measurements and captures
are in `.context/96-responsive-*`.

## Fixed CT1 hashrate bands and legend removal (2026-09-15)

The user superseded the board-relative CT1 hashrate scale and requested removal of
the legend above the ASIC table for every metric.

The fake CT1 simulator defines 335 TH/s per board across 312 ASICs in
`server/fake-ct1/models.go`. Its ASIC endpoint in `rest_api_handler.go` divides that
target across the ASICs and applies +/-3% variation: approximately 1,074 GH/s nominal,
with readings around 1,042–1,106 GH/s while mining and zero when stopped.

The CT1 layout now supplies these fixed **FPO** bands, independent of neighboring
ASIC readings:

| Per-ASIC hashrate | Color | Placeholder meaning |
| --- | --- | --- |
| Below 1,000 GH/s | Red | Below expected minimum |
| 1,000 to below 1,075 GH/s | Amber | Expected minimum met, below ideal |
| 1,075 GH/s and above | Green | Ideal met |

The ideal threshold rounds the simulator's nominal target; the expected minimum is
a provisional lower cutoff. These are UI placeholders, not firmware-validated health
limits. The user will follow up with firmware about guidance and possible API fields.
Missing/non-finite readings retain the unknown color. R2 hashrate and voltage/frequency
retain their relative scales; temperature retains its absolute thresholds. The CT1
hashrate story now includes simulator-sized readings and low/stopped examples.

Verification: all 72 diagnostic tests, changed-file ESLint, TypeScript, and the ProtoOS
production build passed. Chromium confirmed red/amber/green bands, legend removal
across all four metrics in both themes, and viewport-safe popovers. No browser errors
were observed. Local captures and results are in `.context/96-threshold-*`.

## Color module and shared hashrate bands (2026-09-15)

All ASIC color calculations and range selection now live in `Asic/asicColors.ts`.
`metricDisplay.ts` handles measurement normalization, labels, and formatting;
`useAsicPalette.ts` resolves all five theme colors once per grid. The former
`kpis/hooks/asicColor.ts` temperature calculation moved into the color module.

The user also superseded the CT1-only threshold scope: CT1 and R2 now both use the
same FPO 1,000 GH/s expected minimum and 1,075 GH/s ideal threshold. The constants
live in the color module instead of the layout descriptor, with a TODO to obtain
expected/ideal ASIC thresholds from the API after firmware guidance. Temperature,
frequency, and voltage color behavior is unchanged. Values remain in GH/s.

Validation: 76 diagnostic tests pass, including shared CT1/R2 threshold coverage.
Changed-file lint, TypeScript, and the ProtoOS build pass. Browser comparison of all
312 CT1 cell colors across four metrics in both themes matches the pre-refactor
baseline, with no browser errors. Local evidence: `.context/96-color-refactor-*`.

## Short-window popovers and row styling (2026-09-15)

Review found that scrolling popovers could flip above the viewport when neither
side of the ASIC had enough space. Reproduced at 1440 × 360: the popover's top was
-232 px. Both portal modes now share viewport clamping, accounting for the animation's
final transform; scrolling mode adds the document scroll offset after clamping.
ASIC popovers also enable the existing viewport height cap, now supported in both
portal modes, so very short windows allow internal scrolling to every reading.

The user also requested shorter row labels (Temperature and Hashrate), removal of
row bullets, and 4 px of active-background extension on either side of the text.
The row marker prop and its obsolete chart-tooltip arguments were removed.

Verification: 84 targeted diagnostic/popover tests, changed-file lint, TypeScript,
and the ProtoOS build pass. Chromium verified an 8 px viewport margin at 1440 × 360,
internal scrolling at 1440 × 200, trigger following during page scroll, six ASICs in
the live app at 1440 × 420, and container scrolling. The active row measured exactly
4 px of background beyond both text edges; labels and bullet removal were checked.
No browser errors were observed. Evidence: `.context/96-popover-clamp-*`.

Visual follow-up: the user found the 4 px active-row background extension too tight.
Increased it to 8 px on each side while preserving alignment with the other rows.
Chromium confirmed both 8 px gaps and aligned labels; changed-file lint passed.
