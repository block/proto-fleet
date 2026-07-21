---
title: "Rack-selection UX parity with miner selection in Building Management"
date: 2026-07-16
status: draft
type: plan
tracker: https://github.com/block/proto-fleet/issues/758
---

# Rack-selection UX parity with miner selection in Building Management

## Context

Miner selection inside the **Manage Rack** modal received a series of UX
improvements the building-side rack pickers never got:

- **#701** — *Show assigned miners* toggle, id-based eligibility,
  reassignment-behind-a-confirm, Site/Building filter facets.
- **#702 / #718** — site/building placement refinements + assignable-only
  leak fixes.
- **#728** — scoped the pickers to the page-header `SitePicker`
  (+ site-unassigned miners) and removed the redundant Site facet.

`ManageBuildingModal` is a structural mirror of `ManageRackModal`, but its
two rack pickers — bulk `ManageRacksModal` and single `SearchRacksModal` —
are well behind. This plan (issue #758) brings them to parity, adapted to
racks.

### Reference implementation (miner side)

| Piece | File | Notes |
|---|---|---|
| Scope hook | `features/fleetManagement/components/ManageRackModal/useRackMinerScope.ts:1-27` | `siteFilterFromActive(useActiveSite())`; adds `includeUnassigned:true` only for the `"site"` case |
| Selection list | `components/MinerSelectionList.tsx` | toggle @306-314, `PAGE_SIZE=50` server pagination @206, `isReassignment` @496-501, conflict dialog @978-995 |
| Reparent dialog | `features/fleetManagement/components/ManageRackModal/ReparentWarningDialog.tsx:1-37` | count-aware copy |
| Host handler | `ManageRackModal.tsx:562-608` (`handleManageMinersConfirm`), `:415-421` (`promptReparent`) | drives reparent confirm from picker-reported reassignments |

### Current state (rack side)

| File | State |
|---|---|
| `features/buildings/components/ManageRacksModal/ManageRacksModal.tsx` | `listRacks({})` unscoped (`:113-133`); client-side pagination, `PAGE_SIZE=25` (`:36`, `:135-147`); no name box |
| `features/buildings/components/SearchRacksModal/SearchRacksModal.tsx` | `listRacks({})` unscoped (`:93`); fetch-all loop; client-side substring name filter (`:117-124`); single-select |
| `features/buildings/components/rackPickerItem.ts:19-49` | `buildRackPickerItem` already classifies `inThisBuilding` / `inOtherBuilding` / `inOtherSite` and sets `disabled` |
| `features/buildings/components/ManageBuildingModal/ManageBuildingModal.tsx` | scope derives from `building.siteId`; no active-site scope forwarded to pickers |
| `api/useDeviceSets.ts:57-75,427-495` | `listRacks` already accepts + forwards `siteIds`, `includeUnassigned`, `buildingIds`, `includeNoBuilding`, `zones`, `pageSize`/`pageToken` to the RPC |

## Decisions (resolved with product)

1. **Reparenting a rack is allowed** — behind maximum warning. Ineligible
   (other-building / other-site) racks are hidden by default, surfaced only
   by an explicit **Show assigned racks** toggle, flagged with a warning
   icon per row, and gated by a confirmation dialog before commit. The
   dialog copy must state that the rack's **miners move with it** ("Move
   rack {X} and its N miners to {building}?").
2. **Name search is deferred entirely.** There is no server-side name
   search today and we are not adding one — no `nameQuery` proto field, no
   `useDeviceSets` change. The only existing name search is the
   **client-side** substring filter in `SearchRacksModal`. It keeps working
   because `SearchRacksModal` stays on the fetch-all + client-side path (see
   Part C).
3. **Stale header scope on detail pages is fixed at the navigation layer,
   not per-modal.** `ManageBuildingModal` (and `ManageRackModal`) are
   reachable from the headerless `/buildings/:id` / `/racks/:rackId` /
   `/sites/:id` routes, where the persisted SitePicker selection can be an
   unrelated site. Rather than each picker special-casing the mismatch, a
   small **scope-sync** effect on those detail pages overwrites the active
   scope to the entity's own site (leaving `all-sites` untouched). Once the
   header always agrees with the entity, the pickers can use the simple
   global-vs-scoped model and lean on `building.siteId` / `rack.siteId`
   without union or mismatch-fallback logic. This is a **separate PR** (it
   also repairs a pre-existing miner bug — see "Cross-cutting" below) that
   Part B depends on.

## Naming trap

- `includeUnassigned` = **site**-unassigned (no site).
- `includeNoBuilding` = **building**-unassigned (no building).
- "+ unassigned" in the header scope maps to `includeUnassigned` (site
  level), exactly as on the miner side.
- The `useDeviceSets` hook param for zone filtering is named **`zones`**,
  not `zoneKeys` (the proto message is `ZoneKey`, the hook field is
  `zones`).

## Delivery — sequencing

- **Part A** — site scoping. **Shipped** as [#760](https://github.com/block/proto-fleet/pull/760).
- **Scope-sync PR** ([#764](https://github.com/block/proto-fleet/issues/764)) —
  cross-cutting. Also fixes a pre-existing miner-rack-modal bug. **Part B
  depends on it.**
- **Part C** ([#765](https://github.com/block/proto-fleet/issues/765)) — facets
  + server pagination. Independent of scope-sync.
- **Part B** ([#766](https://github.com/block/proto-fleet/issues/766)) — toggle
  + reparent. Built on the scope-sync model; ships last.

## Cross-cutting — scope-sync PR (and the miner-rack-modal fix)

**Separate PR / own issue ([#764](https://github.com/block/proto-fleet/issues/764)).**
On the headerless detail routes
(`/buildings/:id`, `/racks/:rackId`, `/sites/:id` — all outside
`SiteScopeLayout`), the active scope comes from the persisted SitePicker
value, which can point at an unrelated site. A shared effect fixes this at
the source:

- **New** `useSyncScopeToEntity(siteId)` — used by `BuildingPage`,
  `RackOverviewPage`, `SiteDetailPage`. On load: if `activeSite` is a
  *different* site (or `unassigned`), `setActiveSite` to the entity's own
  site; leave `all-sites` untouched. Verified safe against
  `useActiveSite` reconciliation — these routes have no route scope, so the
  route-scope mirror effect early-returns and won't clobber the write, and
  the deleted-site guard passes for a real site.
- Needs the site **slug** (not just id) to build the scoped `ActiveSite`;
  resolve from `ListSites` / `knownSiteSlugById` (the existing slug-reconcile
  effect then keeps it fresh).
- Only ever changes behavior in the deep-link/bookmark case (in-app
  navigation never mismatches), which is exactly when switching context to
  the opened entity is desirable. It is a global-nav behavior change, so it
  wants an explicit product nod.

### Miner selection in the Rack modal — required fix under the new strategy

The **same stale-header bug already exists on the miner side today** (not
introduced by #758). Reproduced: open a rack whose site ≠ the persisted
header site via `/racks/:rackId` and —

- Toggle **off**: the list shows only the rack's *current* members, not the
  rack's site's assignable miners.
- Toggle **on**: the list shows miners from the *header's* site, and hides
  the rack's own site's miners.

Cause: `MinerSelectionList` clamps the default view by `eligibility` (the
rack's site) but drives the toggle-on breadth and the Building/Rack facet
options off the header `scope` (`:669`, `:679`), which is stale here.

**Fix: the scope-sync PR resolves this with no `MinerSelectionList` change.**
Once the header is synced to the rack's site on `/racks/:rackId`, `scope`
== the rack's site, so toggle-on shows the rack's site (within-site
reparent) and the facet options are correct; cross-site reparent still
requires explicitly choosing all-sites, as intended. Acceptance for the
scope-sync PR must include re-running this reproduction on both the rack and
building modals.

### PR 1 — Part A: Site scoping — **shipped as [#760](https://github.com/block/proto-fleet/pull/760)**

Small, low-risk, independently valuable.

- **New** `features/buildings/components/ManageBuildingModal/buildingRackScope.ts`
  — pure `buildingRackScope(buildingSiteId)` helper:
  - real site (id ≠ 0) → `{ siteIds: [id], includeUnassigned: true }`.
  - site-unassigned building (id = 0) → `{ siteIds: [], includeUnassigned: true }`.
- `ManageBuildingModal.tsx` — compute the scope once from `building.siteId`,
  forward a `scope` prop to both `ManageRacksModal` and `SearchRacksModal`.
- Both pickers — pass `siteIds` / `includeUnassigned` from `scope` into
  their `listRacks(...)` calls instead of `{}`.

> **Design note — scope from the building, not the header.** The issue
> proposed mirroring #728's `siteFilterFromActive(useActiveSite())`. In
> review (Codex P2 on #760) that proved unsafe here: `ManageBuildingModal`
> is reachable from the **unscoped** `/buildings/:id` and `/sites/:id`
> routes (outside `SiteScopeLayout`), where `useActiveSite` returns the
> last-persisted header selection — which may be an unrelated site. Opening
> a bookmarked North building while "South" is selected would fetch South's
> racks and hide North's eligible ones. Deriving from `building.siteId`
> instead is authoritative, route-independent, and exactly matches the
> per-row eligibility in `buildRackPickerItem` (same-site + site-unassigned =
> eligible). Consequence: there is no "All sites → empty fetch" case; the
> fetch is always scoped to the building's own site. That is correct for a
> default-view-only picker (no toggle yet). The all-sites → global breadth
> needed for cross-site reparenting arrives in **Part B**, once the
> **scope-sync PR** guarantees the header agrees with the building — so Part
> A needs no revision.

**Tests:** `buildingRackScope` (real-site and unassigned cases);
`ManageRacksModal` + `SearchRacksModal` component tests asserting the scope
reaches `listRacks` (guards against reverting to a whole-org fetch).

### PR 2 — Part C: Filter facets + server-side pagination — [#765](https://github.com/block/proto-fleet/issues/765)

- Add a rack `filterConfig` facet set — adapt, don't copy the miner facets.
  Keep only:
  - **Building** — `buildingIds` / `includeNoBuilding`. Always shown.
  - **Site** — `siteIds`. **Shown only when the header is all-sites**, and
    `site:read`-gated. This mirrors the miner picker exactly
    (`SearchMinersModal.tsx:75` / `ManageMinersModal.tsx:119`:
    `showSiteFilter: !scope`): hide Site when a scope already pins one site
    (the facet would be a no-op), show it when the fetch actually spans sites.
    The only multi-site fetch is the "Show assigned racks" + all-sites global
    broadening from Part B; scope-sync (#764) guarantees a scoped header equals
    the building's site. We accept the harmless no-op in the toggle-off
    all-sites view for strict parity with the miner picker (which keeps the
    facet visible in both toggle states and leans on the empty state), rather
    than adding a rack-only special case. (This *refines* the earlier "drop the
    Site facet" decision, which was written under Part A when the fetch was
    always single-site; Part B's all-sites broadening made Site meaningful.)
  - **Drop the Zone facet — deferred.** Zone labels are unique only *within* a
    building (`ZoneKey { building_id, zone }`; `proto/common/v1/zone.proto`),
    so a label-only zone filter collides across buildings ("Room 2" matches
    every building's "Room 2"). The client still forwards the **deprecated**
    flat `zones: string[]` wildcard path (`listRacks` sends `zones`, not
    `zoneKeys`; `listRackZones` returns `string[]`, no building context), so a
    correct zone facet needs the `zoneKeys`/`ZoneRef` FE migration that the 229
    plan (`docs/plans/2026-05-14-229-...`) explicitly defers to Phase 2. The
    miner picker has **no** zone facet either, so deferring keeps parity. The
    Building facet already provides building-level narrowing. Zone lands as a
    follow-up alongside the `zoneKeys` migration (composite `Building — Zone`
    labels + precise `zoneKeys`, per 229's "richer labels, no cascade" path).
  - Drop **Model / Subnet / Group / Rack** — no rack analog.
- Migrate **`ManageRacksModal`** from client-side slicing to **server-side
  pagination** (`pageSize`/`pageToken`, `PAGE_SIZE=50`) so scope + facets are
  correct across pages. `ManageRacksModal` has no name box (none today).
  - Part B (#766, now merged) hides ineligible rows with a **client-side**
    `items.filter(r => !r.reassignment)` before slicing. That filter does not
    survive server-side pagination and becomes unnecessary — the toggle already
    switches `scope → assignedScope`, so scope governs which rows return.
    **Delete the client-side filter** rather than paginate around it.
- **`SearchRacksModal` stays on fetch-all + client-side name filter** —
  single-select, list is small after site scoping. This is what defers name
  search with zero backend work and no regression.
- Facets compose (AND) with the building-site scope.

**Tests:** facet → request translation (Building maps to
`buildingIds`/`includeNoBuilding`; Site maps to `siteIds` and is only offered
under an all-sites header); scope + facets correct across `ManageRacksModal`
pages; toggle-on/off breadth still correct after removing the client-side
reassignment filter.

### PR 3 — Part B: "Show assigned racks" toggle + reparent — [#766](https://github.com/block/proto-fleet/issues/766)

**Depends on the scope-sync PR ([#764](https://github.com/block/proto-fleet/issues/764))** — the breadth model below assumes the
header always agrees with the building (or is all-sites).

- Add a **Show assigned racks** switch (default OFF) + Info button +
  explainer dialog, mirroring the miner toggle.
  - OFF → show only the assignable set (this building's site + unassigned,
    eligible rows); ineligible rows hidden. Default fetch stays clamped to
    `building.siteId` (Part A), efficient.
  - ON → surface ineligible racks, make them **selectable** behind a reparent
    confirm, flagged with a warning icon + per-row conflict dialog.
- **Breadth of the toggle-on fetch follows the simple global-vs-scoped model**
  (mirrors miners — cross-site reparent requires all-sites):
  - Header **scoped** (guaranteed == building site by scope-sync) → broaden
    to that site only → surfaces same-site, other-building racks
    (within-site reparent). No cross-site.
  - Header **all-sites** → broaden to a global (unscoped) fetch → surfaces
    other-site racks too (cross-site reparent).
  - No union / mismatch special-casing is needed, because scope-sync removed
    the mismatch.
- **New** `features/buildings/components/ManageBuildingModal/RackReparentWarningDialog.tsx`
  — analog of `ReparentWarningDialog`, copy states the rack's miners move
  with it.
- `ManageBuildingModal` drives the confirm from picker-reported
  `reassignedItems`, mirroring `handleManageMinersConfirm` / `promptReparent`.
- Reuse the existing id-based `buildRackPickerItem` classification for
  reassignment flagging.

**Tests:** toggle default-off + surfacing behavior; toggle-on breadth
(scoped → same-site only, all-sites → global); reparent reporting
(`reassignedItems`) to the host modal; dialog gating before commit.

## Acceptance criteria

- [x] Rack pickers fetch scoped to the building's own site
      (+ site-unassigned), correct on scoped and unscoped routes alike.
      (Revised from the original "scope to header SitePicker" — see the PR 1
      design note.)
- [ ] **Scope-sync:** visiting a detail page whose site ≠ the persisted
      header site overwrites the active scope to the entity's site
      (`all-sites` left as-is). The rack-modal reproduction (toggle on/off)
      is correct afterward on **both** the rack and building modals.
- [ ] `Show assigned racks` toggle (default off) hides ineligible racks;
      toggling on surfaces them with warning icons.
- [ ] Toggle-on breadth follows global-vs-scoped: scoped header → same-site
      only; all-sites → cross-site racks surface for reparent.
- [ ] Selecting an already-placed rack prompts a reparent confirm before
      commit; `reassignedItems` reported to the host modal; dialog states
      miners move with the rack.
- [ ] Building facet filters server-side and composes (AND) with the
      building-site scope. Site facet shown only under an all-sites header
      (`site:read`-gated), mirroring the miner picker's `showSiteFilter: !scope`.
      Zone facet deferred (see PR 2 note).
- [ ] `ManageRacksModal` paginates server-side; scope + facets correct
      across pages.
- [ ] Name search unchanged — `SearchRacksModal` client-side filter still
      works; no `nameQuery` added.
- [x] Unit tests: `buildingRackScope` cases + `ManageRacksModal` /
      `SearchRacksModal` scoped-`listRacks` assertions (Part A). Later:
      toggle behavior, facet → request translation, reparent reporting.

## Out of scope

- Model / Subnet / Group / Rack facets (no rack analog).
- **Zone facet — deferred to a follow-up.** Needs the `zoneKeys`/`ZoneRef` FE
  migration (229 plan, Phase 2) to avoid the cross-building label collision;
  the client currently only wires the deprecated flat `zones` wildcard. Miner
  picker has no zone facet either, so deferring holds parity.
- Telemetry-range / error-component facets (possible follow-up).
- Server-side name search / `nameQuery` proto field.

## Files

| File | Change | PR |
|---|---|---|
| `.../ManageBuildingModal/buildingRackScope.ts` (+ `.test.ts`) | **New** — pure `buildingRackScope(siteId)` helper | 1 (shipped) |
| `.../ManageBuildingModal/ManageBuildingModal.tsx` | Compute scope from `building.siteId`, forward to both pickers | 1 (shipped) |
| `.../ManageRacksModal/ManageRacksModal.tsx` (+ `.test.tsx`) | Scope fetch (1); facets + server pagination (2); toggle + reparent flagging (3) | 1,2,3 |
| `.../SearchRacksModal/SearchRacksModal.tsx` (+ `.test.tsx`) | Scope fetch (1); keep client-side name filter (2); toggle + reparent flagging (3) | 1,2,3 |
| `.../components/rackPickerItem.ts` | Reuse/extend classification for reassignment flagging | 3 |
| `.../ManageBuildingModal/RackReparentWarningDialog.tsx` | **New** — reparent confirm | 3 |
| `hooks/useSyncScopeToEntity.ts` + `BuildingPage` / `RackOverviewPage` / `SiteDetailPage` | **New** — sync active scope to the entity's site on headerless detail routes; fixes the pre-existing miner-modal bug | scope-sync ([#764](https://github.com/block/proto-fleet/issues/764)) |
