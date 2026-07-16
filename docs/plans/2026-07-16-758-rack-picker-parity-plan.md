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

## Naming trap

- `includeUnassigned` = **site**-unassigned (no site).
- `includeNoBuilding` = **building**-unassigned (no building).
- "+ unassigned" in the header scope maps to `includeUnassigned` (site
  level), exactly as on the miner side.
- The `useDeviceSets` hook param for zone filtering is named **`zones`**,
  not `zoneKeys` (the proto message is `ZoneKey`, the hook field is
  `zones`).

## Delivery — three PRs (A → C → B)

### PR 1 — Part A: Site scoping (mirror #728)

Small, low-risk, independently valuable.

- **New** `features/buildings/components/ManageBuildingModal/useBuildingRackScope.ts`
  — analog of `useRackMinerScope`:
  - `siteFilterFromActive(useActiveSite())`.
  - For the `"site"` case, add `includeUnassigned: true` (site-unassigned
    racks are the common path into a site).
  - `"all"` → empty filter (no-op fetch, no regression).
  - `"unassigned"` → already `includeUnassigned: true`.
- `ManageBuildingModal.tsx` — read the hook once, forward a `scope` prop to
  both `ManageRacksModal` and `SearchRacksModal`.
- Both pickers — pass `siteIds` / `includeUnassigned` from `scope` into
  their `listRacks(...)` calls instead of `{}`.

**Tests:** `useBuildingRackScope` across the three SitePicker states
(all / site / unassigned); scoped `listRacks` call assertion in both
pickers.

### PR 2 — Part C: Filter facets + server-side pagination

- Add a rack `filterConfig` facet set — adapt, don't copy the miner facets.
  Keep only:
  - **Site** — `siteIds`; hidden when the header scope governs the site
    (per #728 precedent).
  - **Building** — `buildingIds` / `includeNoBuilding`.
  - **Zone** — `zones`.
  - Drop **Model / Subnet / Group** — no rack analog.
- Migrate **`ManageRacksModal`** from client-side slicing to **server-side
  pagination** (`pageSize`/`pageToken`, `PAGE_SIZE=50`) so scope + facets are
  correct across pages. `ManageRacksModal` has no name box (none today).
- **`SearchRacksModal` stays on fetch-all + client-side name filter** —
  single-select, list is small after site scoping. This is what defers name
  search with zero backend work and no regression.
- Facets compose (AND) with the header scope.

**Tests:** facet → request translation; Site facet hidden when scope
governs it; scope + facets correct across `ManageRacksModal` pages.

### PR 3 — Part B: "Show assigned racks" toggle + reparent

- Add a **Show assigned racks** switch (default OFF) + Info button +
  explainer dialog, mirroring the miner toggle.
  - OFF → fetch/show only the assignable set (this building's racks +
    unassigned racks); other-building / other-site rows hidden.
  - ON → surface ineligible racks, make them **selectable** behind a reparent
    confirm, flagged with a warning icon + per-row conflict dialog.
- **New** `features/buildings/components/ManageBuildingModal/RackReparentWarningDialog.tsx`
  — analog of `ReparentWarningDialog`, copy states the rack's miners move
  with it.
- `ManageBuildingModal` drives the confirm from picker-reported
  `reassignedItems`, mirroring `handleManageMinersConfirm` / `promptReparent`.
- Reuse the existing id-based `buildRackPickerItem` classification for
  reassignment flagging.

**Tests:** toggle default-off + surfacing behavior; reparent reporting
(`reassignedItems`) to the host modal; dialog gating before commit.

## Acceptance criteria

- [ ] Rack pickers fetch scoped to the active `SitePicker` site
      (+ site-unassigned); "All sites" is a no-op fetch (no regression).
- [ ] `Show assigned racks` toggle (default off) hides ineligible racks;
      toggling on surfaces them with warning icons.
- [ ] Selecting an already-placed rack prompts a reparent confirm before
      commit; `reassignedItems` reported to the host modal; dialog states
      miners move with the rack.
- [ ] Site / Building / Zone facets filter server-side and compose (AND)
      with the header scope; Site facet hidden when scope governs it.
- [ ] `ManageRacksModal` paginates server-side; scope + facets correct
      across pages.
- [ ] Name search unchanged — `SearchRacksModal` client-side filter still
      works; no `nameQuery` added.
- [ ] Unit tests: `useBuildingRackScope` (three states), scoped `listRacks`
      call, toggle behavior, facet → request translation, reparent reporting.

## Out of scope

- Model / Subnet / Group facets (no rack analog).
- Telemetry-range / error-component facets (possible follow-up).
- Server-side name search / `nameQuery` proto field.

## Files

| File | Change | PR |
|---|---|---|
| `.../ManageBuildingModal/useBuildingRackScope.ts` | **New** — active-site → scope helper | 1 |
| `.../ManageBuildingModal/ManageBuildingModal.tsx` | Read scope, forward to both pickers | 1 |
| `.../ManageRacksModal/ManageRacksModal.tsx` | Scope fetch (1); facets + server pagination (2); toggle + reparent flagging (3) | 1,2,3 |
| `.../SearchRacksModal/SearchRacksModal.tsx` | Scope fetch (1); keep client-side name filter (2); toggle + reparent flagging (3) | 1,2,3 |
| `.../components/rackPickerItem.ts` | Reuse/extend classification for reassignment flagging | 3 |
| `.../ManageBuildingModal/RackReparentWarningDialog.tsx` | **New** — reparent confirm | 3 |
