---
title: Delete the legacyTelemetry projection and migrate consumers to the normalized contract
date: 2026-09-09
status: implementing
type: plan
---

## Summary

GitHub #58 (carved out of #31, epic #26). PR #48 normalized the hardware/telemetry
store onto the entity-keyed contract in `store/contract.ts`, but kept a compatibility
bridge — `store/selectors/legacyTelemetry.ts` — that projects the normalized contract
*back* into the old telemetry view-model (`MinerTelemetryData`, `HashboardTelemetryData`,
`AsicTelemetryData`, `PsuTelemetryData`, `FanTelemetryData`) so downstream consumers kept
working untouched. This plan deletes that bridge and migrates every consumer to read the
normalized contract directly.

**Done when:** `legacyTelemetry.ts` and the legacy `*TelemetryData` types are gone, and
all consumers read the contract shape directly. `tsc`, `eslint`, and `vitest` all pass.

## Two findings that reframe the issue

1. **The stated "design decision" is already moot.** The issue's Note says
   `maxAsicTemp`/`avgAsicTemp` (and inlet/outlet) "are not first-class in the normalized
   contract" and must be derived or promoted. They **already exist** on
   `HashboardTelemetry` (`contract.ts:319-325`): `averageAsicTemperature`,
   `maximumAsicTemperature`, `inletTemperature`, `outletTemperature`. So the hashboard
   temperature migration is a pure field rename, not a design decision. No contract change
   is required.

2. **The one real shape divergence is time-series.** Legacy
   `MetricTimeSeries` (`store/types.ts`) is `{ aggregates, units, values: Value[],
   startTime, endTime, bucketSizeMs }`. Contract `MetricTimeSeries` (`contract.ts:121`) is
   `{ points: MetricPoint[], bucketSizeMs?, units?, aggregates? }` where each
   `MetricPoint` is `{ timestampMs, value, ... }`. The bridge's `metric()` helper flattens
   `points → values` and lifts `points[0].timestampMs → startTime`. Every chart consumer
   that reads `.timeSeries.values` / `.startTime` must be rewritten to read `.points`.
   Aggregates need no change — both sides expose `{ min?, avg?, max? }` Measurements.

## Blast radius (wider than the issue's named list)

The issue lists StatusModal + KPI widgets. The combined `*Data` types
(`HashboardData = HashboardHardwareData & HashboardTelemetryData`, and the Asic/Psu/Fan/
Miner equivalents in `store/types.ts`) are returned by the `useMiner*` hooks and consumed
across the diagnostic feature too. Full inventory:

### Store layer (produces the legacy shape)
- `store/selectors/legacyTelemetry.ts` — **delete entirely** (the 6 `*View` fns + `metric()`).
- `store/hooks/useTelemetry.ts` — 8 hooks map contract entities through `*View`. Return
  contract types directly.
- `store/hooks/useMiner.ts` — `useMiner`/`useMinerHashboard(s)`/`useMinerAsic(s)`/
  `useMinerPsu(s)`/`useMinerFan(s)` merge hardware + `*View(telemetry)`; and
  `useChartDataForMetric` reads `.timeSeries.values`/`.startTime`. Rewrite both.
- `store/types.ts` — **delete** `MinerTelemetryData`, `HashboardTelemetryData`,
  `AsicTelemetryData`, `PsuTelemetryData`, `FanTelemetryData`. Redefine the combined
  `*Data` types (see Design decision 1). Decide the fate of the generic metric primitives
  (`Measurement`, `MetricUnit`, `MetricTelemetry`, `MetricTimeSeries`) — Design decision 2.
- `store/slices/telemetrySlice.ts` — `updatePsuTelemetry`/`updateFanTelemetry` accept
  `Partial<PsuTelemetryData>`/`Partial<FanTelemetryData>` (legacy). Re-type to normalized
  partials (Design decision 3).
- `store/index.ts` — drop the deleted types from the barrel re-export.

### Component consumers (named in issue)
- `components/StatusModal/utils.ts` — `(telemetry as HashboardTelemetryData).avgAsicTemp`,
  `.maxAsicTemp`, `psuTelemetry.temperatureAverage`/`.temperatureHotspot`,
  `(telemetry as FanTelemetryData).percentage`. Rename to contract fields.
- `components/StatusModal/hooks/useComponentTelemetry.ts` — `ComponentTelemetry` union of
  the 4 legacy types. Re-point to contract types.
- KPI widgets `features/kpis/components/{Hashrate,Efficiency,PowerUsage,Temperature}` —
  read `miner?.<metric>?.timeSeries?.aggregates` and `hashboard.<metric>?.timeSeries?.
  aggregates?.avg?.value` + `hashboard.serial`; import `MetricTimeSeries` from the store.
  Aggregates path is unchanged; only the imported `MetricTimeSeries` type origin changes.

### Component consumers (NOT named in issue — found during scoping)
- `features/diagnostic/components/HashboardStatusCard/HashboardStatusCard.tsx` —
  `hashboardData?.avgAsicTemp` / `.maxAsicTemp`.
- `features/diagnostic/components/HashboardTemperature/HashboardTemperature.tsx` —
  `hashboard?.avgAsicTemp`/`.maxAsicTemp`/`.inletTemp`/`.outletTemp`.
- `features/diagnostic/components/HashboardTemperature/Asic/AsicPopover/utility.ts` —
  chart util reading `metric.values` / `metric.startTime` (contract-shape rewrite).
- `features/diagnostic/components/PsuStatusCard/PsuStatusCard.tsx` —
  `psuData?.temperatureAverage` / `.temperatureHotspot`.
- `features/diagnostic/components/FanStatusCard/FanStatusCard.tsx` — `fanData?.percentage`.
- Stories/tests seeding legacy shapes: `PsuStatusCard.stories`, `FanStatusCard.stories`,
  `OtherHardware.stories`, `HashboardStatusCard.stories`, `useCoolingStatus.test`,
  `useMinerStore.test`, `useMiner.test`, `useResetDeviceStateOnMinerChange.test`.

## Field rename map (legacy → contract)

| Legacy (`*TelemetryData`)        | Contract entity field                    |
|----------------------------------|------------------------------------------|
| hashboard `avgAsicTemp`          | `averageAsicTemperature`                 |
| hashboard `maxAsicTemp`          | `maximumAsicTemperature`                 |
| hashboard `inletTemp`            | `inletTemperature`                       |
| hashboard `outletTemp`           | `outletTemperature`                      |
| hashboard `serial`               | derive from hardware side (`.serial`) or `hashboardId` |
| psu `temperatureAverage`         | `averageTemperature`                     |
| psu `temperatureHotspot`         | `hotspotTemperature`                     |
| psu `temperatureAmbient`         | `ambientTemperature`                     |
| psu `id: number`                 | `componentId` (hardware side keeps `id`) |
| fan `percentage`                 | `speedPercent`                           |
| fan `minRpm` / `maxRpm`          | `minimumRpm` / `maximumRpm`              |
| fan `slot` / asic `id`           | hardware side / `asicId`                 |
| timeSeries `values[]` + `startTime` | `points[].value` + `points[0].timestampMs` |

Note: metrics common to both (`hashrate`, `temperature`, `power`, `efficiency`,
`voltage`, `frequency`, psu input/output power/voltage/current) keep the same names —
only their nested `timeSeries` shape changes.

## Design decisions

**Decision 1 — Redefine the combined `*Data` types on the contract.**
`useMiner*` hooks merge hardware + telemetry. Keep that merge, but the telemetry half
becomes the contract entity type. E.g.
`HashboardData = HashboardHardwareData & Omit<HashboardTelemetry, "hashboardId">`
(and Asic/Psu/Fan/Miner analogues, omitting the id key that the hardware side already
provides). This keeps the hook surface intact while consumers read contract field names.
*Open question:* whether to keep the combined types at all or return `{ hardware,
telemetry }` — recommend keeping the flat merge to minimize consumer churn.

**Decision 2 — Keep the generic metric primitives; consolidate their time-series shape.**
`Measurement` / `MetricUnit` / `MetricTelemetry` / `MetricTimeSeries` in `store/types.ts`
are generic, not entity view-models, and back `telemetryUtils`
(`convertAndFormatMeasurement`, `convertValueUnits`, `formatValue`) + the KPI widgets. The
issue only targets the `*TelemetryData` view-models. **But** legacy `MetricTimeSeries`
(`values[]`) is the divergent piece, and nothing will produce it once the bridge is gone.
Recommendation: point telemetry consumers at the contract's `MetricTelemetry`/
`MetricTimeSeries`/`Measurement` and delete the legacy `MetricTimeSeries` (and, if
unreferenced afterward, the legacy `MetricTelemetry`). Verify `convertValueUnits` handles
contract unit strings ("°C", "TH/s", "W") — the legacy `MetricUnit` used preference units
("C"/"F", "kW"). **This is the highest-risk detail** and the most likely source of a unit
mismatch; confirm with a focused test before rippling the rename out.

**Decision 3 — Re-type the slice writers to normalized partials.**
`updatePsuTelemetry(slot, Partial<PsuTelemetry>)` / `updateFanTelemetry(slot,
Partial<FanTelemetry>)`. Their bodies already remap legacy→contract field names
(`input.percentage → current.speedPercent`), so switching the input type mostly *removes*
the remap. All callers are tests/stories — update their seed objects to contract fields.

## Phased execution

Order matters: land the contract-side plumbing first so `tsc` guides the consumer edits.

1. **Types + slice (store core).** Redefine combined `*Data` types (Decision 1), delete the
   5 `*TelemetryData` types, resolve the metric primitives (Decision 2), re-type slice
   writers (Decision 3). Update `store/index.ts` barrel. Expect `tsc` to light up every
   consumer — that's the worklist for steps 2–4.
2. **Store hooks.** Rewrite `useTelemetry.ts` (return contract entities, drop `*View`) and
   `useMiner.ts` (merge contract telemetry; rewrite `useChartDataForMetric` to build chart
   rows from `points[]`). Delete `store/selectors/legacyTelemetry.ts`.
3. **Component consumers.** Apply the rename map to StatusModal (`utils.ts`,
   `useComponentTelemetry.ts`), KPI widgets, and the diagnostic cards + `AsicPopover/
   utility.ts` chart rewrite.
4. **Tests + stories.** Update seed objects to contract field names; fix
   `useMiner.test`, `useCoolingStatus.test`, `useMinerStore.test`, the diagnostic stories.
5. **Verify.** `tsc --noEmit`, `eslint`, targeted `vitest` on store + diagnostic +
   StatusModal + kpis. Confirm chart rendering path (points→chart rows) with the existing
   store/chart tests. Manually sanity-check one KPI + StatusModal render if a dev server is
   available.

## Testing

- Type check is the primary safety net for a rename-heavy migration — run `tsc` after each
  phase (see `[[client-deps-verify-in-conductor]]` for running checks when `npm install`
  is blocked in Conductor).
- Keep/adjust existing store tests (`useMiner.test.tsx`, `useMinerStore.test.ts`); assert
  the chart hook still produces correct `datetime`/value rows from `points[]`.
- Add a focused test for the unit path (Decision 2): a contract `Measurement { units: "°C"
  }` through `convertAndFormatMeasurement` must still convert/format correctly.

## Cross-client / shared-component safety (confirmed)

The rename is contained to `protoOS/store` and its ProtoOS consumers — it cannot reach
protoFleet or the shared components:

- **protoFleet is decoupled.** Zero imports from `@/protoOS/store` in `protoFleet/`. Its
  telemetry is a separate type system — generated protobuf `common.v1.Measurement[]`
  (`@/protoFleet/api/generated/common/v1/measurement_pb`), unrelated to the store's
  `Measurement`/`MetricTimeSeries`/`MetricTelemetry`. The name overlap is coincidental.
- **shared/ does not use the legacy telemetry types.** No `*TelemetryData` imports and no
  store `MetricTimeSeries`/`MetricTelemetry` imports. The only two shared→protoOS imports
  are non-telemetry (`GroupedToaster` → `ResizeablePanel`; `TemperatureValue` → the
  `temperatureUnit` preference). shared's own `AsicData` (`{row,col,value}`) and
  `Measurement` (protobuf) are distinct.
- **The protoOS→shared boundary is already flattened.** Shared chart components
  (`LineChart` via `KpiLineChart`) take the shared `ChartData` type
  (`{datetime, [key]: number}`), which the protoOS hooks *produce*. The rewrite changes how
  `useChartDataForMetric` / `AsicPopover/utility.ts` build `ChartData` (from `points[]`
  instead of `values[]`) but keeps the `ChartData` output type, so shared consumers are
  untouched. Only flattened primitives cross into shared, never the telemetry view-models.

## Risks / out of scope

- **Unit-string mismatch (Decision 2)** — contract vs preference unit strings feeding
  `convertValueUnits`. Highest risk; gate with a test before the wide rename.
- **Chart correctness** — `points[]` may be sparse/unordered where `values[]` assumed a
  dense series anchored at `startTime`; preserve the existing index/interval math.
- **#53** (test coverage for this bridge) closes as won't-fix once this lands.
- Out of scope: the `store/types.ts` STORE_REFACTOR TODO (moving types to `/shared`),
  and any protoFleet-side dashboard panels (they don't consume these ProtoOS hooks).
