---
title: "Fleet page telemetry columns on Sites + Buildings tabs"
date: 2026-06-15
status: draft
type: tdd
tracker: https://github.com/block/proto-fleet/issues/372
---

# Fleet page telemetry columns on Sites + Buildings tabs

## Context

PR #368 landed the unified `/fleet` page with tabbed Sites / Buildings /
Racks / Miners views. `SitesListTable` and `BuildingsListTable` render
real values only for `name`, `site`, and `miners`. Six (sites) /
seven (buildings) telemetry columns — `issues`, `hashrate`,
`efficiency`, `power`, `temperature`, `health` — render the
`INACTIVE_PLACEHOLDER` em-dash.

The Racks tab (`RacksPage` → `DeviceSetList`) already has all of
these columns wired against a clean pattern:

1. List RPC fetches paginated rows (no stats).
2. After list resolves, a batch stats RPC (`GetDeviceSetStats(ids[])`)
   fetches telemetry for every row on the page in one call.
3. Result is a `Map<bigint, DeviceSetStats>` passed alongside the row
   array to per-column renderers in `deviceSetColConfig.tsx`.
4. Each cell gates on its `*ReportingCount` field — zero reporting
   miners renders the em-dash so a partial average isn't displayed
   as if it were fleet-wide.
5. Health column uses `CompositionBar` with three segments
   (`Healthy`, `Needs Attention`, `Offline+Sleeping`).
6. Temperature is a `min/max` range formatted by `formatTempRange`
   honoring the user's F/C preference.

This TDD mirrors that pattern onto Sites + Buildings tabs. The
backend rollups (`GetSiteStats` / `GetBuildingStats`) already exist
and accept `repeated int64 ids`, so the batch-RPC piece is free.
What's missing is (a) temperature aggregation fields on the two
stats messages, and (b) per-component issue counts to drive the
issues column. Both are added in the same proto bump.

Phase 1b grid-view enrichment (#263) shipped via PR #335 and is the
reference for which formatters and reporting-count gates to reuse.

## Scope

In:

- Extend `GetSiteStatsResponse` + `GetBuildingStatsResponse` with
  temperature rollups and component issue counts, matching the
  `DeviceSetStats` field semantics.
- Add `rack_count` to `SiteWithCounts` and `GetSiteStatsResponse`
  so the Sites tab can render a `racks` column without a separate
  RPC. Buildings already has `rackCount`.
- Service-layer SQL: compute `MIN`/`MAX` temperature, component
  issue counts, and rack counts from `MinerStateSnapshot` /
  `device_set` joined on `site_id` / `building_id`.
- New client list-state hooks `useSiteListState` /
  `useBuildingListState` that wrap list-then-batch-stats, mirroring
  `useDeviceSetListState`.
- Container-count columns on both tabs — Sites gets
  `buildings | racks | miners`; Buildings gets `racks | miners`.
- Column config modules for Sites and Buildings tabs, replacing the
  placeholder cells in `SiteList` / `BuildingList`.
- Pass `statsMap` through `FleetSitesPage` / `FleetBuildingsPage`
  to the list components.

Out:

- Action menus on rows (#369 / #370).
- Saved views on Sites / Buildings tabs — Phase 1 keeps saved views
  on the Miners tab only per the master plan.
- Visual redesign of any column; columns and order are fixed by
  #368.
- Per-issue-type filter dropdown on Sites / Buildings tabs — the
  component issue counts ship on the wire so a follow-up can add
  filtering, but the UI dropdown isn't in this scope.

## Decisions

- **Batch RPC, not per-row hooks.** Match the racks pattern. One
  `GetSiteStats(ids)` call per page transition, not 50.
  Per-row `useSiteStats` hooks (used by `BuildingCard` on `/sites`)
  are correct for the grid view where viewport-gating throttles
  off-screen cards, but a table renders all rows synchronously, so
  the batch RPC is the right shape.
- **Temperature lives on `*Stats`, not on `Site` / `Building`.**
  Temperature is a telemetry rollup, not a static attribute. Field
  numbers and names match `DeviceSetStats`
  (`min_temperature_c=7`, `max_temperature_c=8`,
  `temperature_reporting_count` mirrors the existing
  `*_reporting_count` block).
- **Component issue counts on `*Stats` too.** Sites/Buildings get
  the same four counters as racks
  (`control_board_issue_count`, `fan_issue_count`,
  `hash_board_issue_count`, `psu_issue_count`). The issues column
  sums them, matching the rack column exactly. Adding the
  per-component breakdown (vs. a single `brokenCount`) unlocks a
  future issue-type filter on these tabs without a second proto
  bump.
- **Health derivation: `CompositionBar` with three segments.**
  Exact mapping from `deviceSetColConfig.tsx:111-128`:
  `Healthy = hashingCount`, `Needs Attention = brokenCount`,
  `Offline = offlineCount + sleepingCount`. Don't reinvent the
  segmentation — using `CompositionBar` keeps the three tabs
  visually consistent.
- **Reporting-count gating per metric.** Each metric cell renders
  the em-dash when its reporting count is zero, matching the
  pattern in `deviceSetColConfig.tsx` and the existing grid-view
  cells. This avoids the "looks like 0 TH/s but really means no
  miner reported" failure mode.
- **Container counts always show `0`, not em-dash.** Structural
  counts (buildings / racks / miners) are not gated on reporting.
  A site with zero racks has zero racks — that's a real value,
  not "data missing." Em-dash stays reserved for telemetry that
  no miner reported. Counts also render `0` (not blank) when the
  stats fetch is in flight but the list row's
  `SiteWithCounts.{rackCount,…}` fallback covers it.
- **Container count cells link to the filtered child tab.**
  Sites-row `buildings` cell → `/fleet/buildings?site=:id`;
  `racks` → `/fleet/racks?site=:id`; `miners` →
  `/fleet/miners?site=:id`. Buildings-row `racks` →
  `/fleet/racks?building=:id`; `miners` →
  `/fleet/miners?building=:id`. Matches how the `site` link on
  Buildings tab already routes. If a filter param isn't yet
  supported on the target tab, render the count as plain text
  (no link) — don't ship a broken link.
- **One PR for BE + Client.** Proto + service + client land
  together. The list components break without the temp fields,
  and the BE fields are dead weight without the table wiring, so
  splitting would just introduce a coordination window.

## Backend changes

### Proto (`proto/sites/v1/sites.proto`)

Append to `GetSiteStatsResponse`. Use next free field numbers
(don't renumber). Match `DeviceSetStats` semantics:

```protobuf
double min_temperature_c = N;
double max_temperature_c = N+1;
int32 temperature_reporting_count = N+2;
int32 control_board_issue_count = N+3;
int32 fan_issue_count = N+4;
int32 hash_board_issue_count = N+5;
int32 psu_issue_count = N+6;
int32 rack_count = N+7;
```

Append to `SiteWithCounts` as well so the list-row first paint
has a rack count before stats land:

```protobuf
int32 rack_count = M;
```

`buildingCount` and `deviceCount` already exist on
`SiteWithCounts`; only `rack_count` is new.

### Proto (`proto/buildings/v1/buildings.proto`)

Same seven telemetry/issue fields on `GetBuildingStatsResponse`.
Field numbers chosen independently of the Sites message — they
don't have to match. No `rack_count` addition needed —
`BuildingWithCounts` and `GetBuildingStatsResponse` already
expose `rackCount` and `deviceCount`.

### Service layer — factor shared helpers

The three stats handlers (`GetSiteStats`, `GetBuildingStats`,
`GetDeviceSetStats` → `GetCollectionStats`) already share
`devicerollup.AggregateLatestMetrics()` for hashrate / power /
efficiency rollups. Temperature min/max and component error
counts are currently **only** implemented on the collection
(rack) path. Rather than copy that logic into the site + building
handlers, extend the shared helpers so all three consume one
implementation. This is the main lift of the BE work.

**Telemetry rollup — extend the existing shared helper.**

`server/internal/domain/devicerollup/devicerollup.go`:

- Add `MinTemperatureC`, `MaxTemperatureC`, and
  `TemperatureReportingCount` to `MetricsRollup`.
- Compute them inside `AggregateLatestMetrics()` using the same
  NaN/Inf-filter + per-field reporting-count pattern already in
  place for the other metrics. One implementation, three callers.
- Delete the inline temperature loop in
  `collection/service.go` (lines 1055–1244) and have it read
  from the rollup struct instead.

The site + building handlers already call
`AggregateLatestMetrics()` — they just don't read temp out of
the result today. Once the helper produces it, the site +
building stats handlers map the four new fields onto their
response messages with no new SQL.

**Component issue counts — factor and rescope.**

`GetComponentErrorCountsByCollections` lives at
`server/internal/domain/stores/sqlstores/device.go:1461-1502`,
hardcoded to the `device_set_membership` join. The same
aggregation pattern (count open errors grouped by
`error.component_type`) needs to run scoped by `site_id` and
`building_id`.

Two factoring options — recommend **(B)**:

- **(A) New methods.** Add `GetComponentErrorCountsBySites(ids)`
  + `GetComponentErrorCountsByBuildings(ids)` alongside the
  existing collection variant. Three near-identical SQL bodies.

- **(B) Generalize once.** Replace the per-scope methods with
  one `GetComponentErrorCounts(ctx, scope)` where `scope` carries
  the filter (collection IDs / site IDs / building IDs).
  Internally, switch on the scope to attach the right
  `JOIN device_set_membership` vs `WHERE device.site_id IN` vs
  `WHERE device.building_id IN` clause. One SQL skeleton +
  one component-type-to-field mapping. Migrate
  `GetCollectionStats` over to the new shape in the same PR
  to keep the call sites consistent.

Site + building handlers consume the helper and copy the four
counts (`controlBoardIssueCount` / `fanIssueCount` /
`hashBoardIssueCount` / `psuIssueCount`) directly into their
response messages.

**State bucket logic — leave alone, follow-up in #469.**

`GetMinerStateCountsByCollections` (inline SQL in
`sqlstores/device.go:1353-1434`) and the sqlc
`CountMinersByState` query duplicate the bucket priority rules
(`AUTH_NEEDED` → broken, ERROR/REBOOT → broken, ACTIVE +
no-issues → hashing, etc.). Both paths already serve the site
+ building handlers via `GetMinerStateCountsByDeviceIDs`, so
#372 doesn't need to touch this. Out of scope here because
conflating that refactor with #372 risks regressing the live
`health` column on the racks tab — the bucket DRY-up is tracked
in #469 and lands after #372.

**Rack count on sites.** Add to both `ListSites` (so
`SiteWithCounts.rackCount` ships on the initial paint) and
`GetSiteStats` (so live updates reflect rack add/remove without
a page refresh). Query shape: count `device_set` rows of type
`RACK` whose building belongs to the site —
`device_set JOIN building ON device_set.building_id = building.id
WHERE building.site_id = $1 AND device_set.collection_type = RACK`.
Confirm during impl that the existing `building.site_id` index
+ `device_set.building_id` index cover this join; both ship
from #197 / earlier.

**No new RPCs. No new tables.** The
`(site_id, building_id)` indexes from #197 cover the new
filter clauses on the component-error helper. Confirm during
implementation; add an explanatory comment for a follow-up
index only if the query plan shows a seq scan.

### Server-side tests

- `devicerollup_test.go`: extend the existing table-driven
  cases with temperature inputs — present / null / mixed-null /
  all-null — assert min/max + reporting count match the
  non-null subset.
- Component-error helper: table-driven cases over the
  generalized scope parameter (collection / site / building),
  asserting the same component-type-to-field mapping holds for
  all three scopes against shared fixtures.
- `sites/service_stats_test.go` +
  `buildings/service_stats_test.go`: smoke tests asserting the
  four temp + four component-issue fields appear in the
  response and are wired to the helpers, not recomputed inline.

### Server-side tests

- Service-level: unit tests over the SQL aggregator with fixture
  snapshots covering: all-reporting, partial-reporting, none-
  reporting, mixed-component-issues. Assert reporting counts
  match the actual non-null counts and that `MIN`/`MAX` skip
  null-temperature rows.

## Client changes

### Hooks

`client/src/protoFleet/hooks/useSiteListState.ts` (new):

- Mirror `useDeviceSetListState`. Inputs: list-fn, page size, etc.
- On successful list fetch, immediately call
  `getSiteStats({ siteIds: items.map(s => s.site.id) })`.
- Returns `{ sites, statsMap, isLoading, statsError, refetch }`.
- Stats fetch failure does not blank the row list — surface an
  inline retry banner per the established pattern in
  `SiteOverviewSection`.

`client/src/protoFleet/hooks/useBuildingListState.ts` (new): same
shape, against `getBuildingStats`.

### Page wiring

`FleetSitesPage.tsx`:

- Replace direct `useFleetOutletContext` site read with
  `useSiteListState`. Pass `statsMap` to `SiteList`.

`FleetBuildingsPage.tsx`: same.

### Column configs

`client/src/protoFleet/features/fleetManagement/components/SiteList/siteColConfig.tsx`
(new). Exports per-column renderers that take
`{ site, stats }` and return a `ReactNode`:

| Column      | Source field                                    | Render                                                                                              |
|-------------|-------------------------------------------------|-----------------------------------------------------------------------------------------------------|
| buildings   | `stats.buildingCount ?? site.buildingCount`     | count as link to `/fleet/buildings?site=:id`                                                        |
| racks       | `stats.rackCount ?? site.rackCount`             | count as link to `/fleet/racks?site=:id`                                                            |
| miners      | `stats.deviceCount ?? site.deviceCount`         | count as link to `/fleet/miners?site=:id`                                                           |
| issues      | sum of 4 component counts                       | count; `text-core-negative` if > 0                                                                  |
| hashrate    | `totalHashrateThs`, `hashrateReportingCount`    | `formatHashrateOrDash(totalHashrateThs)` gated on reporting count                                   |
| efficiency  | `avgEfficiencyJth`, `efficiencyReportingCount`  | `formatEfficiencyOrDash(avgEfficiencyJth)` gated                                                    |
| power       | `totalPowerKw`, `powerReportingCount`, site capacity | `formatPowerUsedCapacity(totalPowerKw, site.powerCapacityMw)` gated; falls back to capacity-only when no reporting |
| temperature | `min/maxTemperatureC`, `temperatureReportingCount` | `formatTempRange(min, max, unit)` gated                                                          |
| health      | `hashingCount`/`brokenCount`/`offlineCount`/`sleepingCount` | `CompositionBar` with 3 segments matching `deviceSetColConfig`                                |

Sites tab column order:
`name | buildings | racks | miners | issues | hashrate | efficiency | power | temperature | health`
(10 cols total, up from 8). `buildings` + `racks` are new;
`miners` moves from "single count" placeholder to a linked
count.

`BuildingList/buildingColConfig.tsx` (new): same, with three
differences:

- New `racks` column linking to `/fleet/racks?building=:id`
  (count from `stats.rackCount ?? building.rackCount`).
- `miners` cell links to `/fleet/miners?building=:id`.
- Building has no `powerCapacityMw`, so the power column uses
  `formatPowerMwOrDash(totalPowerKw)` (total only).

Buildings tab column order:
`name | site | racks | miners | issues | hashrate | efficiency | power | temperature | health`
(10 cols total, up from 9). `racks` is new.

**Linking helper.** Add `client/src/protoFleet/features/fleetManagement/utils/fleetTabLinks.ts`
exporting `siteTabHref(tab, siteId)` and
`buildingTabHref(tab, buildingId)` so the count cells don't
hand-build URL strings. One place to update if filter param
naming changes on the target tab.

**Filter-param availability.** Confirm during impl which
`?site=` / `?building=` filters the target tabs already
support. Where the filter isn't wired yet, the count cell
renders as plain text (no link) — log a follow-up issue per
missing filter rather than blocking #372 on cross-tab filter
work.

### Component updates

`SiteList.tsx`:

- New prop: `statsMap: Map<bigint, GetSiteStatsResponse>`.
- Replace placeholder branches (lines 114–119) with calls into
  `siteColConfig`. Look up stats via
  `statsMap.get(item.site.id)`; if absent, render em-dash (stats
  for this row haven't loaded yet).

`BuildingList.tsx`: same shape, against `buildingColConfig`.
Replace lines 144–150.

### Tests

- `siteColConfig.test.tsx` / `buildingColConfig.test.tsx`:
  table-driven cases for each metric — value present, reporting
  count zero, partial reporting, all-zero counts, `CompositionBar`
  segment ratios.
- `useSiteListState.test.ts` / `useBuildingListState.test.ts`:
  list fetch triggers stats fetch with correct ID set; stats
  error doesn't clear `sites`; refetch re-fires both.
- Update existing `SiteList.test.tsx` /
  `BuildingList.test.tsx` to construct a `statsMap` fixture and
  assert rendered cells against the new column config.

## Risk

- **SQL performance.** The new aggregations (`MIN`/`MAX` temp,
  four component sums) add per-row work to `GetSiteStats` /
  `GetBuildingStats`. Racks does the same shape against the same
  snapshot table with no observed regression — but for a fleet
  with many sites, the per-page batch could touch a lot of rows.
  Mitigation: confirm the existing `(site_id, building_id)`
  indexes from #197 cover the aggregation filter; if not, add an
  explanatory comment for a follow-up index. Don't add an index
  speculatively.
- **Temperature reporting-count semantics.** A miner that reports
  hashrate but no temperature must increment
  `hashrate_reporting_count` but not
  `temperature_reporting_count`. The per-field counts are
  independent — easy to get wrong if the aggregator naively
  reuses one count. Service-level tests cover this case.
- **Component issue derivation drift.** Avoided by the
  helper-factoring above — a single
  `GetComponentErrorCounts(scope)` keeps the
  component-type-to-field mapping in one place. Drift is now a
  build-time concern, not a runtime one.

- **Rack regression from the helper migration.**
  `GetCollectionStats` (rack tab) gets migrated onto the
  generalized component-error helper + the extended
  `MetricsRollup` in the same PR. If the migration changes
  observable values, the racks tab regresses. Mitigation:
  golden-output tests on `GetCollectionStats` against the
  pre-refactor implementation for representative fixtures
  (mixed-state collections, partial-reporting, all-offline)
  before the refactor lands.

- **Wide tables at narrow viewports.** Sites + Buildings tabs
  go to 10 columns. The racks tab is already 11 and ships
  without obvious truncation issues, so the existing
  `DeviceSetList` layout should hold — but confirm during impl
  at common viewport widths and at the tablet breakpoint. Don't
  pre-emptively add a column-visibility toggle; scope that as a
  follow-up if QA flags it.

## Estimated size

~18–20 files:

- 2 proto edits (`sites.proto` — telemetry/issues + `rack_count`
  on `SiteWithCounts` and `GetSiteStatsResponse`;
  `buildings.proto` — telemetry/issues only).
- `devicerollup.go` + test — extend `MetricsRollup` with temp.
- `sqlstores/device.go` — generalize
  `GetComponentErrorCountsByCollections` to a scoped helper.
- `sites/service.go` — extend `ListSites` + `GetSiteStats` SQL
  with rack count; consume the shared helpers.
- `buildings/service.go` + `collection/service.go` — consume the
  shared helpers.
- Service-level tests for all three handlers.
- 2 new client hooks (`useSiteListState`,
  `useBuildingListState`).
- 2 new column configs (`siteColConfig`, `buildingColConfig`).
- 1 new link helper (`fleetTabLinks.ts`).
- 2 list-component updates (`SiteList`, `BuildingList`).
- 2 page wirings (`FleetSitesPage`, `FleetBuildingsPage`).
- Plus proto regen output.

## Depends on

- #368 (scaffold + placeholder cells)
- #197 (`MinerStateSnapshot.site_id` / `.building_id`)
- #335 (`GetSiteStats` / `GetBuildingStats` batch RPCs)

## Reference

- Pattern source: `RacksPage` →
  `client/src/protoFleet/hooks/useDeviceSetListState.ts` (lines
  72–130) and
  `client/src/protoFleet/components/DeviceSetList/deviceSetColConfig.tsx`
  (lines 62–128).
- Formatters: `client/src/shared/utils/telemetryFormat.ts`.
- Grid-view reference impl (per-row hook, viewport-gated):
  `client/src/protoFleet/features/buildings/components/BuildingCard.tsx`.
- J9 health rule:
  `docs/plans/2026-05-05-multi-site-support-plan.md` lines
  790–793.
