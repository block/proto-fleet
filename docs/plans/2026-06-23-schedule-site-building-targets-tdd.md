---
title: "Schedules: site & building targets + site-filtered selection"
date: 2026-06-23
status: implementing
type: tdd
tracker: TBD
---

# Schedules: site & building targets + site-filtered selection

## Context

The Schedule creation/edit modal's **Apply to** section
([`ScheduleModal.tsx`](../../client/src/protoFleet/features/settings/components/Schedules/ScheduleModal.tsx))
lets an operator target a schedule at **Racks**, **Groups**, and **Miners**.
It does not offer **Sites** or **Buildings**, even though multi-site landed
(#516) and settings is now site-aware (#524). This plan adds Site and
Building as first-class schedule targets and makes the modal honor the topbar
SitePicker selection by **filtering** the available targets.

This builds directly on the #524 toolkit already wired into the modal:
`useActiveSite({})` → `siteFilterFromActive(activeSite)`, threaded into
`RackSelectionModal` / `MinerSelectionList`
([`siteFilter.ts`](../../client/src/protoFleet/components/PageHeader/SitePicker/siteFilter.ts)).

### Why this is full-stack (the gating fact)

Schedule targets are a closed enum. `ScheduleTargetType`
([`proto/schedule/v1/schedule.proto:109`](../../proto/schedule/v1/schedule.proto))
is `UNSPECIFIED | RACK | MINER | GROUP`. The modal's buttons map 1:1 to it
(`buildScheduleRequest`,
[`scheduleValidation.ts:422`](../../client/src/protoFleet/features/settings/components/Schedules/scheduleValidation.ts)),
and the server expands targets → device identifiers **at execution time** in
[`targets/expand.go`](../../server/internal/domain/schedule/targets/expand.go)
(called from `processor.go:511`), switching only on rack/group/miner. Adding
Site/Building buttons in the UI alone would emit targets the server silently
drops. So this needs a proto change + server expansion + UI.

## Core behavior (locked)

**Add Site and Building as target options.** Then mirror the #524 filtering
model already used for racks/miners — no new "lock" mechanism:

1. **No site selected (all sites / unassigned).** Site and Building are
   normal target pickers alongside Rack/Group/Miner. The Site picker lets the
   operator target one or more whole sites; nothing is filtered.
2. **A single site selected in the header (`activeSite.kind === "site"`).**
   - The **Site** target option is **hidden** — you're already operating
     within that site, so targeting "a site" is redundant.
   - **Building / Rack / Miner** pickers are **filtered** to the active site
     (off-site options are simply not offered), exactly as #524 already does
     for racks and miners. This *is* the enforcement: you can't target another
     site's resources because they aren't in the list.
3. **Groups are exempt.** Group targeting stays cross-site: never filtered,
   never hidden, regardless of the header selection — groups are intentionally
   org-wide (accepted #520 decision; groups expose cross-site membership).
   (User decision, 2026-06-23.)
4. **Site/Building targets are dynamic.** Expansion runs at execution time, so
   a Site target resolves to *whatever paired miners are at the site when the
   schedule fires* — added miners are included, removed ones drop. Same for
   Building. No create-time snapshot.
5. **No new URL/scope segment.** Settings routes stay unscoped; the active
   site is read from the store, consistent with #524.
6. **Multi-select.** Site and Building pickers are multi-select, like racks /
   groups / miners. (User decision, 2026-06-23.)
7. **All-sites Building picker includes unassigned.** In all-sites mode the
   Building picker lists every building including site-unassigned ones (no
   filter), matching the rack picker. (User decision, 2026-06-23.)

### Per-target behavior

| Target | All-sites / unassigned | Single site selected |
|---|---|---|
| **Site** | shown — multi-select target picker | **hidden** |
| **Building** | shown — all buildings | shown — filtered to the site; off-site ids pruned |
| **Rack** | all racks (today) | filtered to the site (already #524) |
| **Miner** | all miners (today) | filtered to the site (already #524) |
| **Group** | all groups | **unchanged — cross-site, never filtered** |

This is the same `scope` plumbing #524 added — the new Site/Building pickers
and the Site-button visibility just consume `activeSite` / `scope` that
`ScheduleModal` already computes.

## Backend design

### Proto (regen via `proto-regen`)

Add to `ScheduleTargetType` in `proto/schedule/v1/schedule.proto`:

```proto
SCHEDULE_TARGET_TYPE_SITE = 4;
SCHEDULE_TARGET_TYPE_BUILDING = 5;
```

Append-only enum values — no migration, no `target_id` schema change
(`stores/sqlstores/schedule.go` persists `target_type` int + `target_id`
string generically). `target_id` carries the decimal site/building id.

### Target expansion (`targets/expand.go`)

Add two cases. Reuse the existing device resolver instead of new SQL:
`GetDeviceIdentifiersByOrgWithFilter(ctx, orgID, *MinerFilter)`
([`device.go:195`](../../server/internal/domain/stores/interfaces/device.go))
where `MinerFilter` already carries `SiteIDs` / `BuildingIDs` /
`PairingStatuses` (`device.go:57`).

```go
case SCHEDULE_TARGET_TYPE_SITE:
    siteID := parse(target.TargetId)
    devices := deviceStore.GetDeviceIdentifiersByOrgWithFilter(ctx, orgID,
        &MinerFilter{SiteIDs: []int64{siteID}, PairingStatuses: pairedLikeSet})
case SCHEDULE_TARGET_TYPE_BUILDING:
    buildingID := parse(target.TargetId)
    devices := deviceStore.GetDeviceIdentifiersByOrgWithFilter(ctx, orgID,
        &MinerFilter{BuildingIDs: []int64{buildingID}, PairingStatuses: pairedLikeSet})
```

- **Pairing caveat (must handle):** `GetDeviceIdentifiersByOrgWithFilter`
  defaults to PAIRED-only unless the paired-like set is passed explicitly
  (the #524 device-resolver footgun). Pass the same pairing set the rest of
  the schedule pipeline uses so site/building expansion matches rack/group
  expansion. Confirm what `GetDeviceIdentifiersByDeviceSetID` (rack/group)
  effectively yields and align.
- `Expand` currently takes `collectionStore`; it will also need the device
  store (or a small resolver interface) injected. Keep dedup + order
  semantics identical (targets are a union; duplicates omitted).

### Server validation

`ScheduleTarget.target_type` already has `defined_only` + `not_in: [0]`
buf.validate; the new enum values are automatically accepted. Extend the
service `validateTargets` allowlist (`isValidScheduleTargetType`), the numeric
`target_id` parse case, and `scheduleTargetTypeToString` to cover SITE /
BUILDING; extend the store's `stringToScheduleTargetType` reader for the
"site" / "building" DB strings. (`target_type` is plain `TEXT`, no CHECK
constraint — no migration.) **Done.**

### Power-target conflict filter (done)

`GetRunningPowerTargetScheduleOverlaps`
([`sqlc/queries/schedule.sql`](../../server/sqlc/queries/schedule.sql)) — the
query behind the SET_POWER_TARGET conflict/priority filter
([`command/schedule_conflict_filter.go`](../../server/internal/domain/command/schedule_conflict_filter.go))
— previously resolved only miner/rack/group targets, so a running site/building
power-target schedule's devices weren't protected from a lower-priority
override. Extended to resolve **site** (`device.site_id`) and **building**
(`device.building_id` OR rack membership via `device_set_rack.building_id`,
`device_set_type = 'rack'`, non-deleted set) — mirroring the `MinerFilter`
clauses in `device_filters.go` so the conflict query resolves the *same* miners
that target expansion does. Verified by a DB-backed store test
(`schedule_overlaps_test.go`, run with `DB_PASSWORD=fleet`) covering site,
building-direct, building-via-rack, and an unscoped control miner. **Done.**

## Frontend design

### Form model (`scheduleValidation.ts`)

- `ScheduleFormValues`: add `siteTargetIds: string[]`, `buildingTargetIds: string[]`.
- `createDefaultScheduleFormValues`: both `[]`.
- `buildScheduleRequest`: map them to `ScheduleTargetType.SITE` / `.BUILDING`.
- `createScheduleFormValuesFromSchedule`: parse `SITE` / `BUILDING` targets
  back out (mirror the existing `.filter(targetType === …).map(targetId)`).
- `describeSelectedTargets` + the modal's valid-count memos: include
  site/building counts.

### Modal UI (`ScheduleModal.tsx`)

- New `TargetSelectButton`s for **Sites** and **Buildings** in **Apply to**,
  ordered broad → narrow: Sites → Buildings → Racks → Groups → Miners.
- The **Sites** button renders only when no single site is selected:

  ```ts
  const enforcedSiteId = activeSite.kind === "site" ? activeSite.id : null;
  const showSiteTarget = enforcedSiteId === null;
  ```

- New `SiteSelectionModal` and `BuildingSelectionModal`, modeled on
  `RackSelectionModal`:
  - `SiteSelectionModal` lists sites via `useSites().listSites` (only reachable
    when `showSiteTarget`, i.e. all-sites mode).
  - `BuildingSelectionModal` lists via
    `useBuildings().listBuildings({ siteIds, includeUnassigned })`
    ([`buildings.ts`](../../client/src/protoFleet/api/buildings.ts)), passing
    the active-site `scope`.
  - Both prune selected ids absent from the (scoped) list on load, like
    `RackSelectionModal`.
- Thread the existing `scope` (already computed in `ScheduleModal` from
  `useActiveSite`) into the Building picker, same as racks/miners. Group picker
  unchanged.

### Edit mode

Editing reuses the schedule's stored targets verbatim — no silent re-scoping.
One wrinkle: if a saved schedule has **Site** targets and the operator edits
it while a single site is selected (Site button hidden), those targets must
not be dropped. Preserve `siteTargetIds` through save even when the button is
hidden, and surface a small notice if the schedule has targets outside the
current site (see Open Q2). Same principle as #524 curtailment edit.

## Test plan

**Backend (`targets/expand_test.go`):**
- SITE target → all paired device identifiers for that site, deduped, in
  order; pairing set matches rack/group expansion.
- BUILDING target → all paired devices in that building (direct + via rack).
- Mixed targets (site + rack + miner) dedup correctly (union, no dupes).
- Invalid/zero site/building `target_id` → error, mirroring rack.

**Frontend:**
- `buildScheduleRequest`: site/building ids → `SITE`/`BUILDING` targets;
  round-trips through `createScheduleFormValuesFromSchedule`.
- `SiteSelectionModal` / `BuildingSelectionModal`: all-sites → unfiltered list
  (regression); single site → building list filtered + off-site ids pruned on
  save.
- `ScheduleModal` visibility: with a single header site selected, the **Sites**
  button is **not rendered**, and Building/Rack/Miner pickers receive the site
  filter; the **Group** button stays interactive and unscoped.
- All-sites selected → Sites + Buildings render as free multi-pickers; Groups
  unchanged (regression).
- Edit a schedule that has Site targets while a single site is selected →
  Site targets are preserved on save (not dropped by the hidden button).

**e2e (`proto-fleet-playwright-e2e`):**
- All sites → create: Sites + Buildings pickers list everything.
- Select a site → create: Sites button gone; Buildings list shows only that
  site's buildings; save → schedule fires against the site's miners.
- Switch back to all sites → Sites button returns.

## Risks and mitigations

- **Pairing-set mismatch** between site/building expansion and rack/group
  could include/exclude the wrong miners. Mitigation: explicit pairing set in
  `expand.go`, asserted in `expand_test.go`.
- **Hidden Site button drops existing Site targets in edit mode.** Mitigation:
  preserve `siteTargetIds` on save regardless of button visibility; notice when
  targets fall outside the current site (Open Q2).
- **Empty site/building** targets a schedule at zero miners. Mitigation:
  surface the resolved count in the modal preview; same as an empty rack today.

## Resolved (was open)

- **Cardinality** → multi-select (decision 6).
- **Edit under a selected site** → preserve existing Site/cross-site targets on
  save + notice when targets fall outside the current site (decision in §Edit
  mode).
- **All-sites Building scope** → include unassigned buildings (decision 7).

## Open questions

1. **Tracker.** File the GitHub issue and fill `tracker:` in frontmatter.
