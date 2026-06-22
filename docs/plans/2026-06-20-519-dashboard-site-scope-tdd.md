---
title: "Multi-site: apply site scope to Dashboard data"
date: 2026-06-20
status: draft
type: tdd
tracker: https://github.com/block/proto-fleet/issues/519
---

# Multi-site: apply site scope to Dashboard data

## Context

PR [#516](https://github.com/block/proto-fleet/pull/516) (branch
`issue-511`) shipped the path-based Fleet site-scope foundation:

- `/:siteScope/{dashboard,fleet,groups,energy,activity}` routes plus
  explicit all-sites routes (`/dashboard`, `/fleet`, …).
- `SiteScopeProvider` / `useRouteSiteScope` and the `ActiveSite`
  discriminated union (`{kind:"all"} | {kind:"site";id} | {kind:"unassigned"}`)
  in `client/src/protoFleet/routing/siteScope.tsx`.
- `siteFilterFromActive` / `intersectSiteFilters` wire helpers in
  `client/src/protoFleet/components/PageHeader/SitePicker/siteFilter.ts`.
- Fleet Miners/Racks/Buildings already compose route scope with query
  filters and send `site_ids` / `include_unassigned` to their list RPCs.

That PR **intentionally left Dashboard data org-wide** — the Dashboard
route is scope-aware but the page still reads org-wide metrics. This TDD
covers #519: thread the route active site into every Dashboard panel so
`/{site}/dashboard` shows only that site, `/unassigned/dashboard` shows
unassigned devices (or a deliberate unsupported state), and `/dashboard`
stays all-sites.

PR #516 and the Issue 491 foundation (PR #526) are both merged to `main`;
this branch is rebased on `main`. All work lands behind
`MULTI_SITE_ENABLED` (`VITE_MULTI_SITE_ENABLED`, off in prod), so the
scoped routes remain non-default while we build.

**Delivery: one PR.** All three RPCs + the client wiring land together so
the Dashboard is fully scoped atomically and the "no panel mixes scoped
and org-wide data" criterion is met in a single merge.

## Landed dependency — Issue 491 foundation (PR #526)

PR [#526](https://github.com/block/proto-fleet/pull/526) ("fleet
placement and list filter foundation") is **merged to `main`** (PR
[#527](https://github.com/block/proto-fleet/pull/527), the list-tab filter
UI, is still open and is UI-only — no proto changes, no Dashboard
overlap). #526 was a **different feature** (issue + telemetry **range**
filters on Fleet list rows + a `PlacementRefs` refactor) and verified does
**not** add site filtering to any of the three Dashboard RPCs. So #519 is
still net-new, but the merge changed the ground #519 builds on:

- **Precedent to mirror — `MinerListFilter` gained site fields.** In the
  same `fleetmanagement.proto`, #526 added
  `repeated int64 site_ids = 13;` + `bool include_unassigned = 14;` to
  `MinerListFilter` with exactly the semantics #519 needs. **Reuse these
  field names, comments, and OR/unassigned semantics** when adding fields
  to `GetMinerStateCountsRequest` (and decide telemetry/errors the same
  way) for consistency.
- **`MinerStateSnapshot` placement refactor.** #526 removed
  `site_id`/`site_label`/`rack_label`/`group_labels` (now `reserved`) in
  favor of `common.v1.PlacementRefs placement = 28`. **No Dashboard
  impact** — the Dashboard hooks read counts/metrics/error rollups, not
  snapshot site fields. Flagged only so we don't reintroduce the removed
  fields.
- **Not reusable for #519:** the `server/internal/domain/fleetlistfilter`
  package is telemetry-range + error-component filtering for list-row
  stats only (no `site_id` awareness); `common.v1.DeviceSelector` and
  `common.v1.FleetListTelemetryRangeFilter` are likewise unrelated to
  site scoping. #519 uses the existing `device.site_id` filter path
  instead (see below).
- **Backend already supports the filter.** `MinerFilter{SiteIDs,
  IncludeUnassigned}` and the `device_filters.go` site SQL are in tree and
  unchanged by #526 — `GetMinerStateCounts`'s store method already routes
  to the dynamic builder when a site filter is set.

## The core gap: none of the three Dashboard RPCs filter by site

The Dashboard renders 7 panels backed by **3 RPCs**, none of which
currently accept a site filter:

| Panel(s) | Hook | RPC | Request today | Site filter? |
|---|---|---|---|---|
| FleetHealth | `useFleetCounts` | `FleetManagement.GetMinerStateCounts` | empty message | **No** |
| Hashrate, Power, Efficiency, Temperature, Uptime | `useTelemetryMetrics` | `Telemetry.GetCombinedMetrics` | `DeviceSelector` (`all_devices` \| `device_list`) | **No** |
| FleetErrors (control board / fan / hashboard / PSU) | `useComponentErrors` | `Errors.Query` | `Filter.SimpleFilter` | **No** |

So this issue is **not** a pure frontend wiring task — it requires
proto + Go server changes for all three RPCs. Good news: the device→site
relationship and the filter SQL pattern already exist.

### Reference pattern (already in tree)

`device.site_id` is a direct nullable column (FK to `site`, NULL-stamped
when a site is soft-deleted). The list RPCs filter on it with a
battle-tested clause — e.g. `server/sqlc/queries/building.sql:42-81` and
`server/internal/domain/stores/sqlstores/device_filters.go:361-377`:

```sql
AND (
     (cardinality($site_ids::bigint[]) = 0 AND $include_unassigned::boolean = false)
  OR device.site_id = ANY($site_ids::bigint[])
  OR ($include_unassigned::boolean AND device.site_id IS NULL)
)
```

Wire semantics (mirror `siteFilterFromActive`):

- `all` → `site_ids=[]`, `include_unassigned=false` → no filter (every row).
- `site(id)` → `site_ids=[id]`, `include_unassigned=false`.
- `unassigned` → `site_ids=[]`, `include_unassigned=true` → `site_id IS NULL`.

## Goals

- `/dashboard` renders the current org-wide dashboard, unchanged.
- `/{site}/dashboard` renders only devices assigned to that site, across
  all 7 panels.
- `/unassigned/dashboard` renders only unassigned devices, or a
  deliberate unsupported/empty state for any metric that cannot support
  unassigned yet.
- SitePicker selection + browser refresh preserve the scoped route
  (already handled by PR 516's routing; verify end-to-end).
- No panel silently mixes scoped and org-wide data.

## Non-goals

- Changing all-sites (`/dashboard`) behavior or default flag state.
- Scoping non-Dashboard surfaces (Fleet/Groups/Energy/Activity already
  done or out of scope).
- New telemetry measurement types or panel redesigns.
- Issue 491 list-tab range filters (separate work; see above).

## Data model: how each panel scopes by site (read this first)

We scope **every panel by the device's current site** (`device.site_id`),
using one mechanism: resolve the device identifiers in scope, then filter
each data source by that device set (or, for counts, by the same
`MinerFilter`). This is forced by the data model — see below.

- **FleetHealth counts** (`GetMinerStateCounts` / `GetTotalPairedDevices`)
  filter `device.site_id` directly via `MinerFilter` — already wired
  (RPC #1).
- **Telemetry** (hashrate/power/efficiency/temp/uptime) reads the
  **continuous aggregates** `device_metrics_hourly|daily` and
  `device_status_hourly|daily`. These were verified to **NOT** carry a
  `site_id` column (their `GROUP BY` is `bucket, device_identifier`;
  migration 000047 added `site_id` only to the raw `device_metrics`
  hypertable, `errors`, and `miner_state_snapshots` — not the CAGGs). So
  telemetry cannot filter `site_id` directly. Instead the telemetry
  service resolves device identifiers for the scope
  (`GetDeviceIdentifiersByOrgWithFilter`, which already supports
  `SiteIDs`/`IncludeUnassigned`) and passes them as the existing
  `device_list`. No `telemetry.sql` changes.
- **Errors** (`Errors.Query`) likewise resolves device identifiers for the
  scope and applies them via the existing `device_identifiers` filter.
  No `errors.sql` changes (we do **not** use the `errors.site_id` column).

**Decision — current-membership everywhere (accepted, Option B):** all
panels mean "this site's *current* devices." We do not use point-in-time
row-stamped `site_id` (it isn't available on the telemetry CAGGs anyway,
and mixing current+point-in-time across panels would be confusing). One
consistent mental model; counts/telemetry/errors all agree.

**Empty-resolution guard:** when a site filter resolves to zero device
identifiers, the data sources must return empty — *not* fall through to
the "no device list = all devices" path. Each scoped reader checks for
this explicitly.

## Server / proto changes

### 1. `GetMinerStateCounts` (FleetHealth) — **SMALL**

`FleetHealth` is fed by two store calls and **both** must be filtered or
the bar mixes scopes (e.g. total=org-wide 1000, healthy=site-only 50):

- `GetMinerStateCounts` (per-state breakdown) — store method **already**
  supports the site filter: it routes to the dynamic builder when
  `MinerFilter.SiteIDs`/`IncludeUnassigned` is set
  (`device.go:436-473`, predicate at `device_filters.go:377-393`). No SQL
  change.
- `GetTotalPairedDevices` (the total) — **does not** support site filtering
  today. It runs a *static* sqlc query (`device.sql:38-49`) with only
  status/model filters via the lighter `buildFilterParams`
  (`device.go:619-640`). **This must be extended.** The `device d` table
  (which carries `site_id`) is already joined, so the change is: add
  `site_ids` + `include_unassigned` named args to the sqlc query with the
  standard predicate, and extend `buildFilterParams` to populate them.
  ~15 lines, no new query path.

Wiring:
- **proto** `proto/fleetmanagement/v1/fleetmanagement.proto`
  `GetMinerStateCountsRequest` (currently empty, ~line 438): add
  `repeated int64 site_ids = 1;` + `bool include_unassigned = 2;`. Copy
  the field comments + semantics verbatim from `MinerListFilter.site_ids`
  / `include_unassigned` (same file, added by #526).
- **handler** `server/internal/handlers/fleetmanagement/handler.go`:
  build a `MinerFilter` from the request instead of passing `nil`.
- **service** `server/internal/domain/fleetmanagement/service.go:470-491`:
  pass that filter into **both** `GetTotalPairedDevices` and
  `GetMinerStateCounts`.

### 2. `GetCombinedMetrics` (Hashrate/Power/Efficiency/Temp/Uptime) — **MEDIUM**

**Approach: device-ID resolution (the CAGGs lack `site_id`).** The metric
sources are the continuous aggregates, which have no `site_id` column (see
Data model). So the telemetry service resolves the in-scope device
identifiers and feeds the existing `device_list` query paths — **no
`telemetry.sql` changes.**

**Request shape — flat sibling fields.** `repeated int64 site_ids` +
`bool include_unassigned` directly on `GetCombinedMetricsRequest`, AND'd
with `device_selector`. (Matches the #526 `MinerListFilter` precedent;
dashboard sends `all_devices` + `site_ids`.)

Changes:
- **proto** `proto/telemetry/v1/telemetry.proto` `GetCombinedMetricsRequest`:
  add the two fields. ✅ done
- **domain** add `SiteIDs []int64` + `IncludeUnassigned bool` to
  `models.CombinedMetricsQuery`. ✅ done
- **conversion** `server/internal/handlers/telemetry/conversion.go`: copy
  the fields into the query. ✅ done
- **service** `server/internal/domain/telemetry/service.go` `GetCombinedMetrics`:
  when `SiteIDs`/`IncludeUnassigned` is set, call
  `deviceStore.GetDeviceIdentifiersByOrgWithFilter(ctx, orgID,
  &MinerFilter{SiteIDs, IncludeUnassigned})`, set `query.DeviceIDs` to the
  result, and **short-circuit to an empty `CombinedMetric` if the
  resolution is empty** (don't let empty `DeviceIDs` mean "all"). This one
  spot scopes the line metrics, the temperature/uptime status counts, and
  the live uptime bar uniformly (they all key off `query.DeviceIDs`).
- The service already holds `deviceStore` (used by `appendLiveUptimeBar`).

### 3. `Errors.Query` (FleetErrors) — **MEDIUM**

**Approach: device-ID resolution (Option B — consistent with telemetry).**
We do **not** use the `errors.site_id` column; instead the diagnostics
service resolves in-scope device identifiers and applies them through the
existing `device_identifiers` filter. **No `errors.sql` changes.**

Changes:
- **proto** `proto/errors/v1/errors.proto` `SimpleFilter`: add
  `repeated int64 site_ids` + `bool include_unassigned` (so the client can
  express the scope; the server does the resolution).
- **domain** add the two fields to `models.QueryFilter`; copy them in
  `server/internal/handlers/errorquery/conversions.go`.
- **service** `server/internal/domain/diagnostics/service.go` `Query`: when
  the site filter is set, resolve device identifiers (via the device store)
  and intersect/replace `QueryFilter.DeviceIdentifiers`; **empty resolution
  ⇒ empty result.** Requires giving the diagnostics service a device-ID
  resolver dependency if it lacks one (verify; wire if needed).
- No SQL change; cursor pagination untouched.

### Regen

After proto edits run `/regen` (or the `just`-equivalent; see the
`proto-regen` skill). Commit generated Go + TS in the same PR.

## Client changes

Dashboard has **no `?site=` URL filters**, so it only needs route scope —
no `intersectSiteFilters` required (unlike Fleet pages).

1. **`Dashboard.tsx`** — derive the active scope once and pass it to all
   three hooks:
   ```ts
   const { sites, sitesLoaded } = useSites(); // for knownSiteIds validation
   const knownSiteIds = useMemo(
     () => (sitesLoaded ? buildKnownSiteIds(sites) : undefined), [sites, sitesLoaded]);
   const { activeSite } = useActiveSite({ knownSiteIds });
   const siteFilter = useMemo(() => siteFilterFromActive(activeSite), [activeSite]);
   ```
   Replace the hard-coded `ALL_DEVICES = []` telemetry option with the
   site filter; pass `siteFilter` into `useFleetCounts` and
   `useComponentErrors`.

2. **`useFleetCounts`** — accept `{ siteIds, includeUnassigned }`, set them
   on `GetMinerStateCountsRequest`. Add them to the polling deps / a scope
   key so changing site re-fetches and discards stale responses (follow
   the existing `requestIdRef` pattern).

3. **`useTelemetryMetrics`** — add `siteIds` / `includeUnassigned` options
   set directly on `GetCombinedMetricsRequest` (keep sending
   `all_devices`). Fold the site scope into `scopeKey` (line 36) so the
   in-flight-invalidation reset fires on site change.

4. **`useComponentErrors`** — accept the site filter, set
   `filter.simple.siteIds` / `includeUnassigned`. Fold into the
   `deviceIdentifiersKey`-style scope cache so it re-fetches on change.

5. **Unassigned handling** — all three RPCs support `include_unassigned`
   (`site_id IS NULL`), so every panel works for `/unassigned/dashboard`;
   no per-panel unsupported state is needed. The issue's "deliberate
   unsupported state" fallback therefore does not apply — verify each
   panel renders real unassigned data, never org-wide numbers.

## Testing

- **Unit (client)** — one test per touched hook asserting request
  construction for each scope:
  - `all` → no `site_ids`, `includeUnassigned=false` (telemetry sends
    `all_devices`).
  - `site(7)` → `site_ids=[7n]`.
  - `unassigned` → `includeUnassigned=true`.
  - scope change invalidates stale in-flight responses.
- **Unit / handler (server)** — table tests that proto site fields map to
  the store filter for each of the 3 RPCs; SQL-level test for the errors
  JOIN filter.
- **E2E** (`client/e2eTests/protoFleet`, see `proto-fleet-playwright-e2e`
  skill) — at least one panel verified to change from all-sites to a
  selected site (issue AC). Extend `home.ts` page object for scoped
  dashboard navigation; assert a metric value differs between `/dashboard`
  and `/{site}/dashboard` against fixtures.

## Acceptance criteria (from #519)

- [ ] `/dashboard` renders the org-wide dashboard (unchanged).
- [ ] `/{site}/dashboard` renders only that site's devices, all 7 panels.
- [ ] `/unassigned/dashboard` renders only unassigned devices, or a
      deliberate unsupported/empty state where a metric can't support it.
- [ ] SitePicker selection + refresh preserve the scoped route.
- [ ] Unit tests cover request/filter construction for each hook touched.
- [ ] E2E covers at least one panel changing scope.
- [ ] No panel mixes scoped and org-wide data.

## Implementation order (within the single PR)

Build inside-out so each layer is testable as it lands on the branch:

1. **`GetMinerStateCounts`** end-to-end (proto → handler → service →
   `useFleetCounts` → FleetHealth). Smallest slice; proves the
   proto→server→hook→panel path and the regen flow.
2. **`GetCombinedMetrics`** (proto `site_filter` oneof case → conversion →
   the in-telemetry status bars → `useTelemetryMetrics` → 5 panels).
3. **`Errors.Query`** (proto → domain → `QueryErrors` JOIN →
   `useComponentErrors` → FleetErrors).
4. **`Dashboard.tsx`** integration wiring all three + unit tests + E2E.

## Decisions (resolved during research)

- **Telemetry request shape** → **flat `site_ids` + `include_unassigned`
  fields** on `GetCombinedMetricsRequest` (not a `DeviceSelector` oneof
  case). See RPC #2. **Decided.**
- **`GetTotalPairedDevices`** → **must be changed too** (static query, no
  site support today); small SQL + filter-builder edit. Required so the
  FleetHealth total and breakdown share one scope. See RPC #1.
- **Telemetry & errors filtering** → resolve the in-scope **current**
  device identifiers and apply them through the existing device-list /
  `device_identifiers` paths; the telemetry continuous aggregates have no
  `site_id` column, so no direct-column filter is possible. No
  `telemetry.sql` / `errors.sql` changes. Unassigned = devices currently
  assigned to no site. Site scope is AND'd with any explicit device list.
- **Current-membership everywhere (Option B)** → all panels mean "this
  site's *current* devices." See "Data model" section.
- **#526 reuse** → the `fleetlistfilter` package and `common.v1`
  list-filter types are **not** applicable; #519 uses the
  `device.site_id` filter (counts) and current-membership device
  resolution (telemetry/errors).

- **Pre-000047 NULL `site_id` rows** → **no backfill.** Decided: these
  legacy rows surface under `/unassigned`; immaterial for normal dashboard
  windows (hours–days), not worth a migration. Telemetry/errors
  `include_unassigned` queries (`site_id IS NULL`) will include them as-is.

## Remaining open questions

- **Point-in-time vs current site semantics** — the one item still worth a
  quick eng-review nod (accepted as point-in-time; see Data model). Not
  blocking.
