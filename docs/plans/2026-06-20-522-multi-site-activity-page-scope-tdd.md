---
title: "Multi-site: apply site scope to Activity page"
date: 2026-06-20
status: draft
type: tdd
tracker: https://github.com/block/proto-fleet/issues/522
---

# Multi-site: apply site scope to Activity page

## Context

PR [#516](https://github.com/block/proto-fleet/pull/516) added
path-based Fleet site scope routing. The router already registers both
`/activity` and `/:siteScope/activity` (`activity` is a member of
`SCOPABLE_ROOT_SEGMENTS` in
[`client/src/protoFleet/routing/siteScope.tsx`](../../client/src/protoFleet/routing/siteScope.tsx)),
and primary navigation preserves the selected site scope when moving to
Activity. What is missing is the actual filtering: the Activity page
renders the same org-wide event feed regardless of the route scope.

Much of the backend plumbing already exists from earlier multi-site
phases:

- `activity_log.site_id` already exists — added in migration
  [`000047_add_site_id_to_history_tables.up.sql`](../../server/migrations/000047_add_site_id_to_history_tables.up.sql).
  It is nullable, carries a composite FK to `site(id, org_id)`
  (`ON DELETE SET NULL`), a `CHECK (site_id IS NULL OR organization_id IS NOT NULL)`,
  and the index `idx_activity_log_org_site_created` on
  `(organization_id, site_id, created_at DESC, id DESC)`.
- `InsertActivityLog` in
  [`server/sqlc/queries/activity.sql`](../../server/sqlc/queries/activity.sql)
  already writes `site_id`, and the domain `Event` struct
  ([`server/internal/domain/activity/models/models.go`](../../server/internal/domain/activity/models/models.go))
  already carries `SiteID *int64`.
- The client site-scope toolkit from #516 is reusable as-is:
  `useActiveSite`, `siteFilterFromActive`, `intersectSiteFilters`,
  `scopedPath`.

The remaining work is to thread a site filter through the read path
(proto → sqlc → handler → client) and to resolve how `site_id IS NULL`
should be interpreted, because on `activity_log` that value is
overloaded in a way it is not for `device` / `building` / `device_set_rack`.

## The core design problem: `site_id IS NULL` is overloaded

For `device`, `building`, and `device_set_rack`, `site_id IS NULL` means
exactly one thing: the entity is **unassigned**. The existing buildings/
racks/miners filter pattern leans on that — `include_unassigned=true`
adds back the `site_id IS NULL` rows and they are genuinely "the
unassigned bucket."

On `activity_log`, `site_id IS NULL` conflates two populations:

1. **Org-level events with no site concept** — login/auth, org settings,
   system events. These should appear only in the all-sites feed and
   should *not* leak into `/unassigned/activity`.
2. **Multi-site events that can't fit a single scalar** — device-command
   batches. `logCommandActivity`
   ([`server/internal/domain/command/service.go`](../../server/internal/domain/command/service.go))
   logs one event per batch with only `deviceCount` + `batchID`; a
   batch's device selector can span multiple sites, so a single scalar
   `site_id` cannot represent it. These currently land with `site_id IS NULL`.

So `/unassigned/activity` **cannot** naively mean `site_id IS NULL` — that
would dump every login into the "unassigned" bucket, which is the exact
failure mode the issue warns against ("does not accidentally mean 'events
with no site metadata' unless that is the intended product behavior").

**Decisions (locked):**

- **Direct (single-site) events → scalar `site_id`, Option B.** Site /
  building / collection / device-reassign events stamp the row's
  authoritative `site_id` at write time. The unassigned bucket is
  "site-shaped events whose `site_id` is NULL," with org-level categories
  excluded. Per the writer audit, org-level = `auth`, `system`, `pool`,
  `schedule`, `curtailment` — categories whose emitters have no single-site
  concept (see Server Go > Writer audit for the remaining multi-device
  limitation).
- **Command batch events → derived from `command_on_device_log`, no array,
  no extra stamping.** `command_on_device_log` **already** row-stamps
  `site_id` per device at command-completion time (`command.sql`,
  `UpsertCommandOnDeviceLog`, lines 49-57: *"site_id specifically anchors
  the row to the device's site at completion time so per-site command
  history doesn't shift when the device is later reassigned"*). So a
  command batch's touched sites are recoverable by joining its
  per-device rows. A batch event is "relevant to site X" iff one of its
  `command_on_device_log` rows has `site_id = X`. This gives the
  "show under every site it touched" behavior with **no schema change,
  no writer change, and no backfill**, and keeps the FK / CHECK /
  pagination index that migration 000047 built. (Rejected alternative:
  widening `activity_log.site_id` to an array — loses the composite FK
  and the ordered pagination index, and duplicates data already in
  `command_on_device_log`.)

The Design and test plan below assume this hybrid (scalar for direct
events, `codl` join for batch events).

## Goals

- `/activity` remains org-wide (no behavior change for the unscoped route).
- `/{site}/activity` filters the feed to events relevant to that site,
  using the existing additive `site_ids` / `include_unassigned` filter
  shape already used by ListBuildings / ListRacks / ListMiners. A command
  batch appears under **every** site whose devices it touched (derived
  from `command_on_device_log`).
- `/unassigned/activity` has explicit, documented, tested semantics that
  do **not** silently equal "events with no site metadata."
- The same scope is applied consistently across the three read RPCs:
  `ListActivities`, `CountActivities` (pagination total), and
  `ExportActivities` (CSV).
- Entity/scope links rendered from a scoped Activity feed preserve or
  canonicalize the active site scope via `scopedPath`.
- Tests cover site / unassigned / all-sites filtering semantics at the
  SQL, handler, and client layers, plus an e2e spec for
  `/{site}/activity`.

## Non-goals

- No historical backfill of `site_id` on pre-multi-site rows. They stay
  NULL and behave per the chosen unassigned semantics (consistent with
  the buildings/racks design).
- No new "site" column in the Activity filter-options dropdown UI. Site
  selection is driven by the route/`SitePicker`, not an in-page filter
  facet. (Listed as a possible follow-up, not in scope.)
- No change to *which* events are emitted, only to the `site_id` stamping
  of existing emitters where it is unambiguous and cheap.
- No clickable entity links beyond preserving scope on links that already
  exist / are added — Activity rows are currently read-only text
  (`scopeType` / `scopeLabel`). Adding deep links is a stretch goal
  within this issue's "preserve scope on navigation" criterion, not a
  prerequisite.

## Design

### Filter shape (proto)

Extend `ActivityFilter` in
[`proto/activity/v1/activity.proto`](../../proto/activity/v1/activity.proto),
mirroring `ListBuildingsRequest` exactly so the semantics and validation
match the rest of multi-site:

```protobuf
message ActivityFilter {
    // ... existing fields 1-7 ...

    // Filter by site (OR across IDs). Each entry must be > 0; capped at
    // 1024 to bound request size (matches the rack/building/miner filters).
    repeated int64 site_ids = 8 [(buf.validate.field).repeated = {
        max_items: 1024
        items: { int64: {gt: 0} }
    }];

    // When true, activity rows in the "unassigned" bucket are also
    // included. See TDD for the precise definition of that bucket.
    bool include_unassigned = 9;
}
```

Regen via the `regen` / `proto-regen` skill (do not hand-edit generated
files).

### Query (sqlc)

The filter is **additive**: empty `site_ids` + `include_unassigned=false`
→ no site filter (all-sites). Applied identically to `ListActivityLogs`,
`CountActivityLogs`, and the export query.

A row qualifies through one of **two branches**, chosen by `batch_id`:

- **Non-batch rows (`batch_id IS NULL`)** — direct, single-site events.
  Use the scalar `site_id` with Option B semantics: the unassigned bucket
  is `site_id IS NULL` minus org-level categories (`auth`, `system`,
  `pool`, `schedule`, `curtailment`, `device_command`).
- **Batch rows (`batch_id IS NOT NULL`)** — device-command batches. The
  scalar `site_id` is ignored (always NULL here); relevance is derived
  from `command_on_device_log` (`codl`), which already stamps each
  device's site at completion time. The batch is relevant to a selected
  site iff it has a `codl` row with that `site_id`; it falls in the
  unassigned bucket iff it has a `codl` row with `site_id IS NULL`.

```sql
AND (
     -- all-sites: no filter active
     (cardinality(sqlc.arg('site_ids')::bigint[]) = 0
      AND sqlc.arg('include_unassigned')::boolean = false)

  -- direct events: scalar site_id (Option B)
  OR (a.batch_id IS NULL AND (
          a.site_id = ANY(sqlc.arg('site_ids')::bigint[])
       OR (sqlc.arg('include_unassigned')::boolean
           AND a.site_id IS NULL
           AND a.event_category <> ALL(sqlc.arg('org_level_categories')::text[]))
     ))

  -- command batches: derive touched sites from command_on_device_log
  OR (a.batch_id IS NOT NULL AND EXISTS (
        SELECT 1
        FROM command_on_device_log codl
        JOIN command_batch_log cbl ON cbl.id = codl.command_batch_log_id
        WHERE cbl.uuid = a.batch_id
          AND (
                codl.site_id = ANY(sqlc.arg('site_ids')::bigint[])
             OR (sqlc.arg('include_unassigned')::boolean AND codl.site_id IS NULL)
          )
     ))
)
```

(`a` = `activity_log`.) `org_level_categories` is passed by the Go layer
from a single source of truth (`activity/models`), keeping the
"which categories are org-level" decision in Go, not buried in SQL.

**Index check.** The `EXISTS` joins `cbl.uuid = a.batch_id` and
`codl.command_batch_log_id` and filters `codl.site_id`. Confirm indexes
exist on `command_batch_log.uuid` (unique index expected) and
`command_on_device_log.command_batch_log_id`; `idx_command_on_device_log_site`
already covers `codl.site_id`. Add the `command_batch_log_id` index if
absent (cheap, separate migration). The outer `ORDER BY created_at DESC,
id DESC` still rides `idx_activity_log_org_created`; the `EXISTS` is
evaluated only for the page's candidate rows.

> Rejected alternatives: **Option A** (unassigned = literal
> `site_id IS NULL`) pollutes the bucket with logins/system events.
> **Array column** `site_ids BIGINT[]` on `activity_log` loses the
> composite FK + ordered pagination index and duplicates `codl` data.
> **`is_site_scoped` discriminator column** needs a migration + writer
> changes for no gain over the category exclusion.

> **sqlc array contract** (already documented in `activity.sql`): the
> existing `narg` text[] filters (categories, event_types, …) take `nil`
> (not an empty slice) when inactive — `nil` marshals to SQL NULL, which the
> `IS NULL OR …` guard reads as "no filter". **`site_ids` is the opposite:**
> it is an `arg` whose all-sites case is detected via
> `cardinality(...) = 0`, so the Go store must pass an **empty (non-nil)**
> `bigint[]` when no site filter is active — `nil` would marshal to NULL
> (cardinality NULL, not 0) and silently match nothing. This mirrors the
> ListBuildings / ListRacks / ListMiners stores (`emptyIfNilInt64`).

### Server Go

- Add `SiteIDs []int64` + `IncludeUnassigned bool` to the domain activity
  `Filter`
  ([`server/internal/domain/activity/...`](../../server/internal/domain/activity/)).
- Map them into the sqlc params in the store layer — `site_ids` uses the
  **empty-not-nil** array contract (`emptyIfNilInt64`; see the sqlc array
  contract note above), alongside `org_level_categories` from
  `activity/models`.
- Handler translate layer
  ([`server/internal/handlers/activity/`](../../server/internal/handlers/activity/))
  copies `filter.site_ids` / `filter.include_unassigned` from the proto
  request into the domain filter, for all three RPCs.
- Define `OrgLevelCategories()` in `activity/models` (the single source of
  truth) and pass it to the store as the `org_level_categories` query arg.
  It is backed by an unexported array and returns a fresh copy per call so
  the set can't be mutated by callers.
- **Writer audit** (the scope-sensitive sweep). Confirm `SiteID` stamping
  per emitter so the direct-event branch is meaningful:
  - Already stamping (keep): device→site reassign (`sites/service.go`),
    building/site/rack CRUD and assignment (`buildings/service.go`,
    `sites/service.go`, `collection/service.go` create/save-rack/assign).
  - **Newly stamped:** rack slot set/clear (`collection/service.go`
    `SetRackSlotPosition` / `ClearRackSlotPosition`) — single-rack events
    that previously left `site_id` NULL and so leaked into the unassigned
    bucket. They now stamp the rack's `site_id` (nil only when the rack is
    genuinely unassigned). Likewise, **site-scoped curtailment lifecycle
    rows** (`curtailment/service.go` `emitUpdateAuditTrail` /
    `emitAdminTerminateAuditTrail`) now stamp the event's site (recovered
    from `ScopeJSON`) so they match the `curtailment_started` row, instead
    of only the start being site-attributed.
  - Org-level (leave NULL; category is in `OrgLevelCategories()`):
    `auth`, `system`, `pool`, `schedule`, `curtailment`, `device_command`.
    Two categories are **mixed**, handled by branch not by blanket
    exclusion: (a) site-scoped `curtailment` stamps `site_id` (above) and is
    filtered by site — only whole-org / device curtailments stay NULL and
    lean on this list; (b) `device_command` BATCH rows carry a `batch_id`
    and are scoped via the `codl` EXISTS branch below, so the org-level list
    only affects the direct (non-batch) command audits. `auth`, `system`,
    `pool`, `schedule` never have a single-site concept.
  - **Device-command batches (`command/service.go`): no change.** They
    keep `site_id` NULL; the read query derives their touched sites from
    `command_on_device_log`, which already stamps per-device site at
    completion time. No new stamping, no single-site limitation, no
    backfill.
  - **Non-batch command audits** (`command_preflight_blocked`,
    `command_filter_skip`) have no `batch_id` and no single site (they span
    the requested device set), so they are org-level (`device_command`)
    and surface only in the all-sites feed — not the unassigned bucket.
  - **Also newly stamped (single-site, cheap):** `update_collection` and
    `delete_collection` (`collection/service.go`, from the collection's
    `Placement.Site`) and building delete (`buildings/service.go`
    `DeleteBuilding`, from the building's site captured pre-delete via
    `GetBuildingSiteID`).
  - **Known limitation (follow-up):** the genuinely multi-device events
    still write `site_id` NULL with a non-org-level category and so fall in
    the unassigned bucket — `fleet_management` miner rename / unpair, and
    `collection` add/remove-devices. These span multiple sites, so a clean
    fix needs per-event scope stamping or explicit scope metadata — out of
    scope here, tracked as a follow-up.

### Client

`ActivityPage`
([`client/src/protoFleet/features/activity/pages/ActivityPage.tsx`](../../client/src/protoFleet/features/activity/pages/ActivityPage.tsx))
adopts the canonical #516 consumption pattern already used by `RacksPage`
/ `FleetBuildingsPage`:

1. Obtain `knownSiteIds` (fetch sites, or read from an outlet context if
   one is available on this route).
2. `const { activeSite } = useActiveSite({ knownSiteIds })`.
3. `const scopeFilter = useMemo(() => siteFilterFromActive(activeSite), [activeSite])`.
4. Pass `scopeFilter.siteIds` (as `bigint[]`) + `scopeFilter.includeUnassigned`
   into `useActivity`, `useExportActivity`, and `useActivityFilterOptions`
   so the feed, pagination count, CSV export, and filter facets all agree.

Activity does **not** support a `?site=` deep-link query param (decided —
route scope is the only entry point), so `intersectSiteFilters` is not
used here; the scope filter is passed straight through.

Plumb the two new params through the three client API hooks
([`api/useActivity.ts`](../../client/src/protoFleet/api/useActivity.ts),
[`api/useExportActivity.ts`](../../client/src/protoFleet/api/useExportActivity.ts),
[`api/useActivityFilterOptions.ts`](../../client/src/protoFleet/api/useActivityFilterOptions.ts))
and into the `ActivityFilter` they build.

**Link scope preservation.** Any link rendered from a row (scope label →
Fleet/Rack/Building/Site, or future deep links) is wrapped in
`scopedPath(href, activeSite)` so navigating out of a scoped Activity feed
keeps the operator in-scope, matching `buildingTabHref`'s pattern.

## Test plan

**SQL (sqlc query tests / store tests):**

*Direct (non-batch) events:*
- all-sites: empty `site_ids` + `include_unassigned=false` returns every
  org row (regression — no behavior change).
- single site: only rows with matching scalar `site_id`.
- multi site: OR across `site_ids`.
- unassigned (Option B): direct rows with `site_id IS NULL` **and** a
  site-shaped category; **excludes** org-level (`auth` / `system` / `pool` /
  `schedule` / `curtailment`). A site-stamped collection event (rack slot)
  appears under its site, never here.
- site + unassigned combined: union of the two.

*Command batch events (the `codl` join):*
- a batch touching sites {A, B} appears under `/{A}`, under `/{B}`, and in
  all-sites — but **not** under `/{C}`.
- a batch touching only unassigned (`codl.site_id IS NULL`) devices appears
  in the unassigned bucket and all-sites, not under any specific site.
- a mixed batch (site A + unassigned device) appears under `/{A}` **and**
  in the unassigned bucket.
- a batch event whose `codl` rows don't exist yet (initiated-before-
  completion, see Risks) matches no specific site until completion.

*Cross-cutting:*
- `CountActivityLogs` total matches the filtered `ListActivityLogs`
  cardinality for every case above (pagination correctness), including the
  batch branch.

**Handler:**
- proto `site_ids` / `include_unassigned` map through to the domain filter
  for `ListActivities`, `CountActivities`, `ExportActivities`.
- validation: `site_ids` entries `> 0`, capped at 1024.

**Client:**
- `useActivity` / `useExportActivity` / `useActivityFilterOptions` include
  `siteIds` + `includeUnassigned` derived from the active site.
- `/activity` (all-sites) sends an empty site filter.
- `/{site}/activity` sends that site id; scope round-trips through
  `useActiveSite`.
- row links carry the active scope via `scopedPath`.

**e2e** (`proto-fleet-playwright-e2e` skill):
- `/{site}/activity` renders a scoped feed; switching the SitePicker
  updates both the URL and the feed; `/activity` stays org-wide.

## Risks and mitigations

- **Unassigned semantics regression** — wrong choice silently pollutes the
  unassigned feed with logins. Mitigation: explicit category-exclusion
  test cases above; decision locked at eng review before coding the query.
- **Count/list/export divergence** — applying the filter to only some read
  paths yields a wrong pagination total or an export that disagrees with
  the on-screen feed. Mitigation: single shared domain `Filter`, applied
  to all three; cross-checked in tests.
- **Batch event before its `codl` rows exist** — the `*.initiated` event
  is logged at dispatch, before any `command_on_device_log` row is written
  at completion, so a freshly-initiated batch matches no specific site
  until devices complete. Mitigation: low impact — the `*.completed` row
  (which has `codl` rows) does match, and the client groups initiated +
  completed by `batch_id`, so the grouped entry still surfaces in the
  scoped feed. Documented; covered by a test case.
- **Batch `EXISTS` query cost** — the per-row `EXISTS` join across
  `command_batch_log` / `command_on_device_log`. Mitigation: the join keys
  are indexed (`cbl.uuid`, `codl.command_batch_log_id`, `codl.site_id`);
  the `EXISTS` runs only for the page's candidate rows after the
  `ORDER BY ... LIMIT`. Add the `command_batch_log_id` index if missing.
- **sqlc empty-vs-nil array footgun** — passing `[]` instead of `nil`
  matches nothing. Mitigation: reuse the existing nil-contract already
  enforced for the other array filters; covered by the all-sites
  regression test.

## Resolved decisions

1. **Unassigned semantics (direct events) → Option B.** Category-aware
   NULL bucket: site-shaped rows only; org-level categories (`auth`,
   `system`, `pool`, `schedule`, `curtailment`, `device_command`) excluded.
   Rack-slot collection events and site-scoped curtailment lifecycle rows
   are stamped with their site so they scope correctly.
2. **Command batch site scope → derived from `command_on_device_log`.**
   No array column, no extra stamping, no backfill. A batch appears under
   every site it touched. Keeps the FK / CHECK / pagination index intact.
3. **`?site=` deep-links on Activity → not supported.** Route scope is the
   only entry point; `intersectSiteFilters` is not used.
4. **Permission scoping → out of scope for this PR.** `PermActivityRead`
   stays org-wide (empty ResourceContext); scoping is client/query-driven.
   A site-scoped permission check, if wanted, is a separate RBAC follow-up.

## Open questions

_None blocking._ The `command_batch_log_id` index existence (see Query >
Index check) is a verify-and-maybe-add detail for implementation, not a
design decision.
